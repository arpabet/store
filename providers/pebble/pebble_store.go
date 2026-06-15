/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package pebblestore

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"github.com/cockroachdb/pebble/v2"
	"go.arpabet.com/store"
	"io"
	"reflect"
	"time"
)

var PebbleStoreClass = reflect.TypeOf((*implPebbleStore)(nil))

type implPebbleStore struct {
	name  string
	db     *pebble.DB
	hub    *store.WatchHub

	// pebble has no multi-op transaction in its basic API, so read-modify-write
	// operations (CAS, increment, touch, versioned set) are serialized here to
	// make them atomic within the process. Striped by key hash so writers to
	// different keys proceed in parallel.
	locks store.StripedMutex
}

func New(name string, dataDir string, opts *pebble.Options) (*implPebbleStore, error) {

	db, err := OpenDatabase(dataDir, opts)
	if err != nil {
		return nil, err
	}

	return &implPebbleStore{name: name, db: db, hub: store.NewWatchHub()}, nil
}

func FromDB(name string, db *pebble.DB) *implPebbleStore {
	return &implPebbleStore{name: name, db: db, hub: store.NewWatchHub()}
}

func (t*implPebbleStore) Interface() store.ManagedDataStore {
	return t
}

func (t*implPebbleStore) BeanName() string {
	return t.name
}

func (t*implPebbleStore) Destroy() error {
	return t.db.Close()
}

func (t*implPebbleStore) Get(ctx context.Context) *store.GetOperation {
	return &store.GetOperation{DataStore: t, Context: ctx}
}

func (t*implPebbleStore) Set(ctx context.Context) *store.SetOperation {
	return &store.SetOperation{DataStore: t, Context: ctx}
}

func (t*implPebbleStore) CompareAndSet(ctx context.Context) *store.CompareAndSetOperation {
	return &store.CompareAndSetOperation{DataStore: t, Context: ctx}
}

func (t *implPebbleStore) Increment(ctx context.Context) *store.IncrementOperation {
	return &store.IncrementOperation{DataStore: t, Context: ctx, Initial: 0, Delta: 1}
}

func (t *implPebbleStore) Touch(ctx context.Context) *store.TouchOperation {
	return &store.TouchOperation{DataStore: t, Context: ctx}
}

func (t*implPebbleStore) Remove(ctx context.Context) *store.RemoveOperation {
	return &store.RemoveOperation{DataStore: t, Context: ctx}
}

func (t*implPebbleStore) Enumerate(ctx context.Context) *store.EnumerateOperation {
	return &store.EnumerateOperation{DataStore: t, Context: ctx}
}

func (t*implPebbleStore) Features() store.Capability {
	return store.TTLCapability | store.AtomicCapability | store.OrderedCapability |
		store.WatchCapability | store.BatchAtomicCapability
}

func (t*implPebbleStore) Watch(ctx context.Context) *store.WatchOperation {
	return &store.WatchOperation{DataStore: t, Context: ctx}
}

func (t*implPebbleStore) WatchRaw(ctx context.Context, prefix []byte, cb func(*store.WatchEvent) bool) error {
	return t.hub.Watch(ctx, prefix, cb)
}

func (t*implPebbleStore) GetRaw(ctx context.Context, key []byte, ttlPtr *int, versionPtr *int64, required bool) ([]byte, error) {

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

// readEnvelope fetches and decodes the stored envelope for key, treating expired
// entries as absent. The returned value is copied out of pebble-managed memory.
func (t*implPebbleStore) readEnvelope(key []byte) (version, expiresAt int64, value []byte, found bool, err error) {
	raw, closer, gerr := t.db.Get(key)
	if gerr != nil {
		if gerr == pebble.ErrNotFound {
			return 0, 0, nil, false, nil
		}
		return 0, 0, nil, false, gerr
	}
	v, exp, val, _ := store.DecodeEnvelope(raw)
	if store.IsExpired(exp) {
		return 0, 0, nil, false, closer.Close()
	}
	out := make([]byte, len(val))
	copy(out, val)
	return v, exp, out, true, closer.Close()
}

func (t*implPebbleStore) SetRaw(ctx context.Context, key, value []byte, ttlSeconds int) error {
	t.locks.Lock(key)
	oldVersion, _, _, _, err := t.readEnvelope(key)
	if err != nil {
		t.locks.Unlock(key)
		return err
	}
	newVersion := oldVersion + 1
	enc := store.EncodeEnvelope(newVersion, store.ExpiryFromTtl(ttlSeconds), value)
	if err := t.db.Set(key, enc, WriteOptions); err != nil {
		t.locks.Unlock(key)
		return err
	}
	t.locks.Unlock(key)

	t.notify(key, value, store.WatchSet, newVersion)
	return nil
}

func (t *implPebbleStore) IncrementRaw(ctx context.Context, key []byte, initial, delta int64, ttlSeconds int) (prev int64, err error) {
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
	if err = t.db.Set(key, enc, WriteOptions); err != nil {
		t.locks.Unlock(key)
		return prev, err
	}
	t.locks.Unlock(key)

	t.notify(key, buf, store.WatchSet, newVersion)
	return prev, nil
}

func (t*implPebbleStore) CompareAndSetRaw(ctx context.Context, key, value []byte, ttlSeconds int, version int64) (bool, error) {
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
	} else if version != 0 { // for non-existent record the expected version is 0
		t.locks.Unlock(key)
		return false, nil
	}

	newVersion := oldVersion + 1
	enc := store.EncodeEnvelope(newVersion, store.ExpiryFromTtl(ttlSeconds), value)
	if err := t.db.Set(key, enc, WriteOptions); err != nil {
		t.locks.Unlock(key)
		return false, err
	}
	t.locks.Unlock(key)

	t.notify(key, value, store.WatchSet, newVersion)
	return true, nil
}

func (t *implPebbleStore) TouchRaw(ctx context.Context, key []byte, ttlSeconds int) error {
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
	return t.db.Set(key, enc, WriteOptions)
}

func (t*implPebbleStore) RemoveRaw(ctx context.Context, key []byte) error {
	t.locks.Lock(key)
	if err := t.db.Delete(key, WriteOptions); err != nil {
		t.locks.Unlock(key)
		return err
	}
	t.locks.Unlock(key)

	t.notify(key, nil, store.WatchDelete, 0)
	return nil
}

func (t*implPebbleStore) notify(key, value []byte, eventType store.WatchEventType, version int64) {
	k := make([]byte, len(key))
	copy(k, key)
	var val []byte
	if value != nil {
		val = make([]byte, len(value))
		copy(val, value)
	}
	t.hub.Notify(&store.WatchEvent{Key: k, Value: val, Type: eventType, Version: version})
}

func (t*implPebbleStore) EnumerateRaw(ctx context.Context, prefix, seek []byte, batchSize int, onlyKeys bool, reverse bool, cb func(entry *store.RawEntry) bool) error {
	if reverse {
		var cache []*store.RawEntry
		err := t.doEnumerateRaw(ctx, prefix, seek, batchSize, onlyKeys, func(entry *store.RawEntry) bool {
			cache = append(cache, entry)
			return true
		})
		if err != nil {
			return err
		}
		n := len(cache)
		for j := n-1; j >= 0; j-- {
			if !cb(cache[j]) {
				break
			}
		}
		return nil
	} else {
		return t.doEnumerateRaw(ctx, prefix, seek, batchSize, onlyKeys, cb)
	}
}

func (t*implPebbleStore) doEnumerateRaw(ctx context.Context, prefix, seek []byte, batchSize int, onlyKeys bool, cb func(entry *store.RawEntry) bool) (err error) {

	iter, err := t.db.NewIter(&pebble.IterOptions{
		LowerBound:  seek,
	})
	if err != nil {
		return err
	}

	for iter.First(); iter.Valid(); iter.Next() {

		err = ctx.Err()
		if err != nil {
			iter.Close()
			return err
		}

		if !bytes.HasPrefix(iter.Key(), prefix) {
			break
		}

		version, expiresAt, val, _ := store.DecodeEnvelope(iter.Value())
		if store.IsExpired(expiresAt) {
			continue
		}

		key := make([]byte, len(iter.Key()))
		copy(key, iter.Key())

		re := store.RawEntry{
			Key:     key,
			Ttl:     store.TtlFromExpiry(expiresAt),
			Version: version,
		}

		if !onlyKeys {
			re.Value = make([]byte, len(val))
			copy(re.Value, val)
		}

		if !cb(&re) {
			break
		}

	}

	return iter.Close()
}

func (t*implPebbleStore) First() ([]byte, error) {
	iter, err := t.db.NewIter(&pebble.IterOptions{})
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	if !iter.First() {
		return nil, nil
	}
	key := iter.Key()
	dst := make([]byte, len(key))
	copy(dst, key)
	return dst, nil
}

func (t*implPebbleStore) Last() ([]byte, error) {
	iter, err := t.db.NewIter(&pebble.IterOptions{})
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	if !iter.Last() {
		return nil, nil
	}
	key := iter.Key()
	dst := make([]byte, len(key))
	copy(dst, key)
	return dst, nil
}

func (t*implPebbleStore) Compact(discardRatio float64) error {
	first, err := t.First()
	if err != nil {
		return err
	}
	last, err := t.Last()
	if err != nil {
		return err
	}
	if first == nil || last == nil {
		return nil // empty database, nothing to compact
	}
	return t.db.Compact(context.Background(), first, last, true)
}

func writeBinary(w io.Writer, b []byte) error {
	var lenBuf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(lenBuf[:], uint64(len(b)))
	if _, err := w.Write(lenBuf[:n]); err != nil {
		return err
	}
	_, err := w.Write(b)
	return err
}

func (t*implPebbleStore) Backup(w io.Writer, since uint64) (uint64, error) {
	snap := t.db.NewSnapshot()
	defer snap.Close()
	iter, err := snap.NewIter(&pebble.IterOptions{})
	if err != nil {
		return 0, err
	}

	for iter.First(); iter.Valid(); iter.Next() {

		if err := writeBinary(w, iter.Key()); err != nil {
			iter.Close()
			return 0, err
		}

		if err := writeBinary(w, iter.Value()); err != nil {
			iter.Close()
			return 0, err
		}

	}

	return uint64(time.Now().Unix()), iter.Close()
}

func (t*implPebbleStore) Restore(r io.Reader) error {

	if err := t.DropAll(); err != nil {
		return err
	}

	br := bufio.NewReader(r)

	readBinary := func() ([]byte, error) {
		size, err := binary.ReadUvarint(br)
		if err != nil {
			return nil, err
		}
		buf := make([]byte, size)
		if _, err := io.ReadFull(br, buf); err != nil {
			if err == io.ErrUnexpectedEOF {
				return nil, ErrInvalidFormat
			}
			return nil, err
		}
		return buf, nil
	}

	for {

		key, err := readBinary()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		value, err := readBinary()
		if err != nil {
			if err == io.EOF {
				return ErrInvalidFormat
			}
			return err
		}

		err = t.db.Set(key, value, WriteOptions)
		if err != nil {
			return err
		}
	}

	return nil
}

func (t*implPebbleStore) DropAll() error {
	first, err := t.First()
	if err != nil {
		return err
	}
	last, err := t.Last()
	if err != nil {
		return err
	}
	return t.db.DeleteRange(first, append(last, 0xFF), WriteOptions)
}

func (t*implPebbleStore) DropWithPrefix(prefix []byte) error {

	last := append(prefix, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF)

	return t.db.DeleteRange(prefix, last, WriteOptions)
}

func (t*implPebbleStore) Instance() interface{} {
	return t.db
}