/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package rosedbstore

import (
	"bytes"
	"context"
	"encoding/binary"
	"github.com/pkg/errors"
	"github.com/rosedblabs/rosedb/v2"
	"go.arpabet.com/store"
	"io"
	"reflect"
	"time"
)

var RosedbStoreClass = reflect.TypeOf((*implRosedbStore)(nil))

type implRosedbStore struct {
	name string
	db   *rosedb.DB
	hub  *store.WatchHub

	// rosedb ops are individually safe, but read-modify-write (versioned set,
	// CAS, increment, touch) must be serialized per key across operations.
	// Striped by key hash so writers to different keys proceed in parallel.
	locks store.StripedMutex
}

func New(name string, dataDir string, options ...Option) (*implRosedbStore, error) {
	if name == "" {
		return nil, errors.New("empty bean name")
	}
	db, err := OpenDatabase(dataDir, options...)
	if err != nil {
		return nil, err
	}
	return &implRosedbStore{name: name, db: db, hub: store.NewWatchHub()}, nil
}

func FromDB(name string, db *rosedb.DB) *implRosedbStore {
	return &implRosedbStore{name: name, db: db, hub: store.NewWatchHub()}
}

func (t *implRosedbStore) Interface() store.ManagedDataStore {
	return t
}

func (t *implRosedbStore) BeanName() string {
	return t.name
}

func (t *implRosedbStore) Destroy() error {
	return t.db.Close()
}

func (t *implRosedbStore) Features() store.Capability {
	// native TTL (disk-level expiry, so no sweeper), versioning via envelope,
	// native ordered iteration, native atomic batches; watch is hub-emulated.
	return store.TTLCapability | store.AtomicCapability | store.OrderedCapability |
		store.WatchCapability | store.BatchAtomicCapability
}

func (t *implRosedbStore) Get(ctx context.Context) *store.GetOperation {
	return &store.GetOperation{DataStore: t, Context: ctx}
}

func (t *implRosedbStore) Set(ctx context.Context) *store.SetOperation {
	return &store.SetOperation{DataStore: t, Context: ctx}
}

func (t *implRosedbStore) Batch(ctx context.Context) *store.BatchOperation {
	return &store.BatchOperation{DataStore: t, Context: ctx}
}

func (t *implRosedbStore) CompareAndSet(ctx context.Context) *store.CompareAndSetOperation {
	return &store.CompareAndSetOperation{DataStore: t, Context: ctx}
}

func (t *implRosedbStore) Increment(ctx context.Context) *store.IncrementOperation {
	return &store.IncrementOperation{DataStore: t, Context: ctx, Initial: 0, Delta: 1}
}

func (t *implRosedbStore) Touch(ctx context.Context) *store.TouchOperation {
	return &store.TouchOperation{DataStore: t, Context: ctx}
}

func (t *implRosedbStore) Remove(ctx context.Context) *store.RemoveOperation {
	return &store.RemoveOperation{DataStore: t, Context: ctx}
}

func (t *implRosedbStore) Enumerate(ctx context.Context) *store.EnumerateOperation {
	return &store.EnumerateOperation{DataStore: t, Context: ctx}
}

func (t *implRosedbStore) Watch(ctx context.Context) *store.WatchOperation {
	return &store.WatchOperation{DataStore: t, Context: ctx}
}

func (t *implRosedbStore) WatchRaw(ctx context.Context, prefix []byte, cb func(*store.WatchEvent) bool) error {
	return t.hub.Watch(ctx, prefix, cb)
}

func (t *implRosedbStore) notify(key, value []byte, eventType store.WatchEventType, version int64) {
	k := cloneBytes(key)
	var val []byte
	if value != nil {
		val = cloneBytes(value)
	}
	t.hub.Notify(&store.WatchEvent{Key: k, Value: val, Type: eventType, Version: version})
}

// put writes enc under key, using native TTL when ttlSeconds > 0 so rosedb
// performs disk-level expiry.
func (t *implRosedbStore) put(key, enc []byte, ttlSeconds int) error {
	if ttlSeconds > 0 {
		return t.db.PutWithTTL(key, enc, time.Duration(ttlSeconds)*time.Second)
	}
	return t.db.Put(key, enc)
}

// readEnvelope fetches and decodes the stored envelope for key, treating expired
// entries as absent (native expiry normally removes them first).
func (t *implRosedbStore) readEnvelope(key []byte) (version, expiresAt int64, value []byte, found bool, err error) {
	raw, gerr := t.db.Get(key)
	if gerr != nil {
		if gerr == rosedb.ErrKeyNotFound {
			return 0, 0, nil, false, nil
		}
		return 0, 0, nil, false, gerr
	}
	v, exp, val, _ := store.DecodeEnvelope(raw)
	if store.IsExpired(exp) {
		return 0, 0, nil, false, nil
	}
	return v, exp, cloneBytes(val), true, nil
}

func (t *implRosedbStore) GetRaw(ctx context.Context, key []byte, ttlPtr *int, versionPtr *int64, required bool) ([]byte, error) {
	version, expiresAt, value, found, err := t.readEnvelope(key)
	if err != nil {
		return nil, err
	}
	if !found {
		if required {
			return nil, store.ErrNotFound
		}
		return nil, nil
	}
	if ttlPtr != nil {
		*ttlPtr = store.TtlFromExpiry(expiresAt)
	}
	if versionPtr != nil {
		*versionPtr = version
	}
	return value, nil
}

func (t *implRosedbStore) SetRaw(ctx context.Context, key, value []byte, ttlSeconds int) error {
	t.locks.Lock(key)
	oldVersion, _, _, _, err := t.readEnvelope(key)
	if err != nil {
		t.locks.Unlock(key)
		return err
	}
	newVersion := oldVersion + 1
	enc := store.EncodeEnvelope(newVersion, store.ExpiryFromTtl(ttlSeconds), value)
	if err := t.put(key, enc, ttlSeconds); err != nil {
		t.locks.Unlock(key)
		return err
	}
	t.locks.Unlock(key)

	t.notify(key, value, store.WatchSet, newVersion)
	return nil
}

func (t *implRosedbStore) CompareAndSetRaw(ctx context.Context, key, value []byte, ttlSeconds int, version int64) (bool, error) {
	t.locks.Lock(key)
	oldVersion, _, _, found, err := t.readEnvelope(key)
	if err != nil {
		t.locks.Unlock(key)
		return false, err
	}
	if found {
		if oldVersion != version {
			t.locks.Unlock(key)
			return false, nil
		}
	} else if version != 0 {
		t.locks.Unlock(key)
		return false, nil
	}
	newVersion := oldVersion + 1
	enc := store.EncodeEnvelope(newVersion, store.ExpiryFromTtl(ttlSeconds), value)
	if err := t.put(key, enc, ttlSeconds); err != nil {
		t.locks.Unlock(key)
		return false, err
	}
	t.locks.Unlock(key)

	t.notify(key, value, store.WatchSet, newVersion)
	return true, nil
}

func (t *implRosedbStore) IncrementRaw(ctx context.Context, key []byte, initial, delta int64, ttlSeconds int) (prev int64, err error) {
	t.locks.Lock(key)
	oldVersion, _, val, _, err := t.readEnvelope(key)
	if err != nil {
		t.locks.Unlock(key)
		return 0, err
	}
	counter := initial
	if len(val) >= 8 {
		counter = int64(binary.BigEndian.Uint64(val))
	}
	prev = counter
	counter += delta

	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(counter))
	newVersion := oldVersion + 1
	enc := store.EncodeEnvelope(newVersion, store.ExpiryFromTtl(ttlSeconds), buf)
	if err = t.put(key, enc, ttlSeconds); err != nil {
		t.locks.Unlock(key)
		return prev, err
	}
	t.locks.Unlock(key)

	t.notify(key, buf, store.WatchSet, newVersion)
	return prev, nil
}

func (t *implRosedbStore) TouchRaw(ctx context.Context, key []byte, ttlSeconds int) error {
	t.locks.Lock(key)
	defer t.locks.Unlock(key)

	oldVersion, _, val, found, err := t.readEnvelope(key)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	enc := store.EncodeEnvelope(oldVersion, store.ExpiryFromTtl(ttlSeconds), val)
	return t.put(key, enc, ttlSeconds)
}

func (t *implRosedbStore) RemoveRaw(ctx context.Context, key []byte) error {
	t.locks.Lock(key)
	if err := t.db.Delete(key); err != nil {
		t.locks.Unlock(key)
		return err
	}
	t.locks.Unlock(key)

	t.notify(key, nil, store.WatchDelete, 0)
	return nil
}

func (t *implRosedbStore) SetBatchRaw(ctx context.Context, entries []store.RawEntry) error {
	if len(entries) == 0 {
		return nil
	}

	keys := make([][]byte, len(entries))
	for i := range entries {
		keys[i] = entries[i].Key
	}
	unlock := t.locks.LockMany(keys...)

	type notice struct {
		key, value []byte
		version    int64
	}
	notices := make([]notice, 0, len(entries))

	batch := t.db.NewBatch(rosedb.DefaultBatchOptions)

	for i := range entries {
		e := &entries[i]
		// read the current version via the batch (db.Get would deadlock — NewBatch
		// holds db.mu); batch.Get also sees this batch's own pending writes, so
		// duplicate keys within the batch version monotonically.
		var base int64
		if raw, gerr := batch.Get(e.Key); gerr == nil {
			v, _, _, _ := store.DecodeEnvelope(raw)
			base = v
		} else if gerr != rosedb.ErrKeyNotFound {
			_ = batch.Rollback()
			unlock()
			return gerr
		}
		newVersion := base + 1
		enc := store.EncodeEnvelope(newVersion, store.ExpiryFromTtl(e.Ttl), e.Value)
		var perr error
		if e.Ttl > 0 {
			perr = batch.PutWithTTL(e.Key, enc, time.Duration(e.Ttl)*time.Second)
		} else {
			perr = batch.Put(e.Key, enc)
		}
		if perr != nil {
			_ = batch.Rollback()
			unlock()
			return perr
		}
		notices = append(notices, notice{key: e.Key, value: e.Value, version: newVersion})
	}

	if err := ctx.Err(); err != nil {
		_ = batch.Rollback()
		unlock()
		return err
	}
	if err := batch.Commit(); err != nil {
		unlock()
		return err
	}
	unlock()

	for _, n := range notices {
		t.notify(n.key, n.value, store.WatchSet, n.version)
	}
	return nil
}

func (t *implRosedbStore) EnumerateRaw(ctx context.Context, prefix, seek []byte, batchSize int, onlyKeys bool, reverse bool, cb func(entry *store.RawEntry) bool) error {
	if reverse {
		var cache []*store.RawEntry
		if err := t.ascend(ctx, prefix, seek, onlyKeys, func(entry *store.RawEntry) bool {
			cache = append(cache, entry)
			return true
		}); err != nil {
			return err
		}
		for j := len(cache) - 1; j >= 0; j-- {
			if !cb(cache[j]) {
				break
			}
		}
		return nil
	}
	return t.ascend(ctx, prefix, seek, onlyKeys, cb)
}

func (t *implRosedbStore) ascend(ctx context.Context, prefix, seek []byte, onlyKeys bool, cb func(entry *store.RawEntry) bool) error {
	var cbErr error
	t.db.AscendGreaterOrEqual(seek, func(k, v []byte) (bool, error) {
		if err := ctx.Err(); err != nil {
			cbErr = err
			return false, nil
		}
		if !bytes.HasPrefix(k, prefix) {
			return false, nil // sorted: past the prefix block
		}
		version, expiresAt, val, _ := store.DecodeEnvelope(v)
		if store.IsExpired(expiresAt) {
			return true, nil
		}
		re := store.RawEntry{
			Key:     cloneBytes(k),
			Ttl:     store.TtlFromExpiry(expiresAt),
			Version: version,
		}
		if !onlyKeys {
			re.Value = cloneBytes(val)
		}
		return cb(&re), nil
	})
	return cbErr
}

func (t *implRosedbStore) Compact(discardRatio float64) error {
	return t.db.Merge(true)
}

func (t *implRosedbStore) Backup(w io.Writer, since uint64) (uint64, error) {
	var werr error
	t.db.Ascend(func(k, v []byte) (bool, error) {
		if werr = writeBinary(w, k); werr != nil {
			return false, werr
		}
		if werr = writeBinary(w, v); werr != nil {
			return false, werr
		}
		return true, nil
	})
	return uint64(time.Now().Unix()), werr
}

func (t *implRosedbStore) Restore(r io.Reader) error {
	if err := t.DropAll(); err != nil {
		return err
	}
	br := newByteReader(r)
	for {
		key, err := readBinary(br)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		value, err := readBinary(br)
		if err != nil {
			return err
		}
		// reapply native TTL from the envelope expiry: rosedb has no sweeper, so
		// without PutWithTTL a restored TTL entry would be hidden by the envelope
		// but never physically reclaimed. Already-expired entries are dropped.
		_, expiresAt, _, _ := store.DecodeEnvelope(value)
		if store.IsExpired(expiresAt) {
			continue
		}
		var perr error
		if expiresAt > 0 {
			remaining := time.Until(time.Unix(expiresAt, 0))
			if remaining < time.Second {
				remaining = time.Second
			}
			perr = t.db.PutWithTTL(key, value, remaining)
		} else {
			perr = t.db.Put(key, value)
		}
		if perr != nil {
			return perr
		}
	}
	return nil
}

func (t *implRosedbStore) DropAll() error {
	unlock := t.locks.LockAll()
	defer unlock()
	var keys [][]byte
	t.db.Ascend(func(k, v []byte) (bool, error) {
		keys = append(keys, cloneBytes(k))
		return true, nil
	})
	return t.deleteKeysBatch(keys)
}

func (t *implRosedbStore) DropWithPrefix(prefix []byte) error {
	unlock := t.locks.LockAll()
	defer unlock()
	var keys [][]byte
	t.db.AscendGreaterOrEqual(prefix, func(k, v []byte) (bool, error) {
		if !bytes.HasPrefix(k, prefix) {
			return false, nil
		}
		keys = append(keys, cloneBytes(k))
		return true, nil
	})
	return t.deleteKeysBatch(keys)
}

// deleteKeysBatch removes the given keys in a single atomic rosedb batch. Keys
// must be collected before the batch is opened (NewBatch holds db.mu, so
// iterating during an open batch would deadlock).
func (t *implRosedbStore) deleteKeysBatch(keys [][]byte) error {
	if len(keys) == 0 {
		return nil
	}
	batch := t.db.NewBatch(rosedb.DefaultBatchOptions)
	for _, k := range keys {
		if err := batch.Delete(k); err != nil {
			_ = batch.Rollback()
			return err
		}
	}
	return batch.Commit()
}

func (t *implRosedbStore) Instance() interface{} {
	return t.db
}

func cloneBytes(b []byte) []byte {
	out := make([]byte, len(b))
	copy(out, b)
	return out
}
