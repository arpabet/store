/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package boltstore

import (
	"bytes"
	"context"
	"encoding/binary"
	"github.com/boltdb/bolt"
	"github.com/pkg/errors"
	"go.arpabet.com/store"
	"io"
	"os"
	"reflect"
)

var BoltStoreClass = reflect.TypeOf((*implBoltStore)(nil))

type implBoltStore struct {
	name   string
	db     *bolt.DB
	hub    *store.WatchHub

	dataFile string
	dataFilePerm os.FileMode
	options []Option
}

func New(name string, dataFile string, dataFilePerm os.FileMode, options... Option) (*implBoltStore, error) {

	if name == "" {
		return nil, errors.New("empty bean name")
	}

	db, err := OpenDatabase(dataFile, dataFilePerm, options...)
	if err != nil {
		return nil, err
	}

	return &implBoltStore{
		name: name,
		db: db,
		hub: store.NewWatchHub(),
		dataFile: dataFile,
		dataFilePerm: dataFilePerm,
		options: options,
	}, nil
}

func FromDB(name string, db *bolt.DB) *implBoltStore {
	return &implBoltStore{name: name, db: db, hub: store.NewWatchHub()}
}

func (t *implBoltStore) Interface() store.ManagedDataStore {
	return t
}

func (t*implBoltStore) BeanName() string {
	return t.name
}

func (t*implBoltStore) Destroy() error {
	return t.db.Close()
}

func (t*implBoltStore) Features() store.Capability {
	return store.TTLCapability | store.AtomicCapability | store.OrderedCapability |
		store.WatchCapability | store.BatchAtomicCapability
}

func (t*implBoltStore) Get(ctx context.Context) *store.GetOperation {
	return &store.GetOperation{DataStore: t, Context: ctx}
}

func (t*implBoltStore) Set(ctx context.Context) *store.SetOperation {
	return &store.SetOperation{DataStore: t, Context: ctx}
}

func (t *implBoltStore) Increment(ctx context.Context) *store.IncrementOperation {
	return &store.IncrementOperation{DataStore: t, Context: ctx, Initial: 0, Delta: 1}
}

func (t*implBoltStore) CompareAndSet(ctx context.Context) *store.CompareAndSetOperation {
	return &store.CompareAndSetOperation{DataStore: t, Context: ctx}
}

func (t *implBoltStore) Touch(ctx context.Context) *store.TouchOperation {
	return &store.TouchOperation{DataStore: t, Context: ctx}
}

func (t*implBoltStore) Remove(ctx context.Context) *store.RemoveOperation {
	return &store.RemoveOperation{DataStore: t, Context: ctx}
}

func (t*implBoltStore) Enumerate(ctx context.Context) *store.EnumerateOperation {
	return &store.EnumerateOperation{DataStore: t, Context: ctx}
}

func (t*implBoltStore) Watch(ctx context.Context) *store.WatchOperation {
	return &store.WatchOperation{DataStore: t, Context: ctx}
}

func (t*implBoltStore) WatchRaw(ctx context.Context, prefix []byte, cb func(*store.WatchEvent) bool) error {
	return t.hub.Watch(ctx, prefix, cb)
}

func (t*implBoltStore) GetRaw(ctx context.Context, fullKey []byte, ttlPtr *int, versionPtr *int64, required bool) ([]byte, error) {

	var version, expiresAt int64
	var val []byte
	var found bool

	bucket, key := t.parseKey(fullKey)
	err := t.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		if b == nil {
			return nil
		}
		version, expiresAt, val, found = readEnvelope(b, key)
		return nil
	})

	if err != nil {
		return nil, err
	}

	if !found {
		if required {
			return nil, os.ErrNotExist
		}
		return nil, nil
	}

	if ttlPtr != nil {
		*ttlPtr = store.TtlFromExpiry(expiresAt)
	}
	if versionPtr != nil {
		*versionPtr = version
	}

	return val, nil
}

func (t*implBoltStore) parseKey(fullKey []byte) ([]byte, []byte) {
	i := bytes.IndexByte(fullKey, BucketSeparator)
	if i == -1 {
		return fullKey, []byte{}
	} else {
		return fullKey[:i], fullKey[i+1:]
	}
}

// readEnvelope decodes the stored value for key, treating expired entries as
// absent. The returned value is copied out of the bolt-managed memory.
func readEnvelope(b *bolt.Bucket, key []byte) (version, expiresAt int64, value []byte, found bool) {
	raw := b.Get(key)
	if raw == nil {
		return 0, 0, nil, false
	}
	v, exp, val, _ := store.DecodeEnvelope(raw)
	if store.IsExpired(exp) {
		return 0, 0, nil, false
	}
	out := make([]byte, len(val))
	copy(out, val)
	return v, exp, out, true
}

func (t*implBoltStore) SetRaw(ctx context.Context, fullKey, value []byte, ttlSeconds int) error {

	if t.db.IsReadOnly() {
		return ErrDatabaseReadOnly
	}

	bucket, key := t.parseKey(fullKey)
	var newVersion int64
	err := t.db.Update(func(tx *bolt.Tx) error {

		b, err := tx.CreateBucketIfNotExists(bucket)
		if err != nil {
			return err
		}

		oldVersion, _, _, _ := readEnvelope(b, key)
		newVersion = oldVersion + 1
		enc := store.EncodeEnvelope(newVersion, store.ExpiryFromTtl(ttlSeconds), value)
		return b.Put(key, enc)
	})

	if err == nil {
		t.notify(fullKey, value, store.WatchSet, newVersion)
	}
	return err
}

func (t *implBoltStore) IncrementRaw(ctx context.Context, fullKey []byte, initial, delta int64, ttlSeconds int) (prev int64, err error) {

	if t.db.IsReadOnly() {
		return 0, ErrDatabaseReadOnly
	}

	bucket, key := t.parseKey(fullKey)
	var newVersion int64
	var counter int64
	err = t.db.Update(func(tx *bolt.Tx) error {

		b, err := tx.CreateBucketIfNotExists(bucket)
		if err != nil {
			return err
		}

		oldVersion, _, val, _ := readEnvelope(b, key)
		counter = initial
		if len(val) >= 8 {
			counter = int64(binary.BigEndian.Uint64(val))
		}
		prev = counter
		counter += delta

		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, uint64(counter))
		newVersion = oldVersion + 1
		enc := store.EncodeEnvelope(newVersion, store.ExpiryFromTtl(ttlSeconds), buf)
		return b.Put(key, enc)
	})

	if err == nil {
		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, uint64(counter))
		t.notify(fullKey, buf, store.WatchSet, newVersion)
	}
	return
}

func (t*implBoltStore) CompareAndSetRaw(ctx context.Context, fullKey, value []byte, ttlSeconds int, version int64) (bool, error) {

	if t.db.IsReadOnly() {
		return false, ErrDatabaseReadOnly
	}

	bucket, key := t.parseKey(fullKey)
	var updated bool
	var newVersion int64
	err := t.db.Update(func(tx *bolt.Tx) error {

		b, err := tx.CreateBucketIfNotExists(bucket)
		if err != nil {
			return err
		}

		oldVersion, _, _, found := readEnvelope(b, key)
		if found {
			if oldVersion != version {
				return nil
			}
		} else if version != 0 { // for non-existent record the expected version is 0
			return nil
		}

		newVersion = oldVersion + 1
		enc := store.EncodeEnvelope(newVersion, store.ExpiryFromTtl(ttlSeconds), value)
		if err := b.Put(key, enc); err != nil {
			return err
		}
		updated = true
		return nil
	})

	if err == nil && updated {
		t.notify(fullKey, value, store.WatchSet, newVersion)
	}
	return updated, err
}

func (t *implBoltStore) TouchRaw(ctx context.Context, fullKey []byte, ttlSeconds int) error {

	if t.db.IsReadOnly() {
		return ErrDatabaseReadOnly
	}

	bucket, key := t.parseKey(fullKey)
	return t.db.Update(func(tx *bolt.Tx) error {

		b := tx.Bucket(bucket)
		if b == nil {
			return nil
		}

		oldVersion, _, val, found := readEnvelope(b, key)
		if !found {
			return nil
		}

		enc := store.EncodeEnvelope(oldVersion, store.ExpiryFromTtl(ttlSeconds), val)
		return b.Put(key, enc)
	})
}

func (t*implBoltStore) RemoveRaw(ctx context.Context, fullKey []byte) error {

	if t.db.IsReadOnly() {
		return ErrDatabaseReadOnly
	}

	bucket, key := t.parseKey(fullKey)
	err := t.db.Update(func(tx *bolt.Tx) error {

		b, err := tx.CreateBucketIfNotExists(bucket)
		if err != nil {
			return err
		}

		return b.Delete(key)
	})

	if err == nil {
		t.notify(fullKey, nil, store.WatchDelete, 0)
	}
	return err
}

func (t*implBoltStore) notify(fullKey, value []byte, eventType store.WatchEventType, version int64) {
	key := make([]byte, len(fullKey))
	copy(key, fullKey)
	var val []byte
	if value != nil {
		val = make([]byte, len(value))
		copy(val, value)
	}
	t.hub.Notify(&store.WatchEvent{Key: key, Value: val, Type: eventType, Version: version})
}

func (t*implBoltStore) EnumerateRaw(ctx context.Context, prefix, seek []byte, batchSize int, onlyKeys bool, reverse bool, cb func(entry *store.RawEntry) bool) error {
	if reverse {
		var cache []*store.RawEntry
		err := t.doEnumerateRaw(prefix, seek, batchSize, onlyKeys, func(entry *store.RawEntry) bool {
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
		return t.doEnumerateRaw(prefix, seek, batchSize, onlyKeys, cb)
	}
}

func (t*implBoltStore) doEnumerateRaw(prefix, seek []byte, batchSize int, onlyKeys bool, cb func(entry *store.RawEntry) bool) error {

	// for API compatibility with other storage impls (PnP)
	if !bytes.HasPrefix(seek, prefix) {
		return ErrInvalidSeek
	}

	if len(prefix) == 0 {
		return t.enumerateAll(prefix, seek, onlyKeys, cb)
	}

	bucketPrefix, _ := t.parseKey(prefix)
	bucketSeek, _ := t.parseKey(seek)

	if !bytes.Equal(bucketPrefix, bucketSeek) {
		return errors.Errorf("seek has bucket '%s' whereas prefix has bucket '%s'", string(bucketSeek), string(bucketPrefix))
	}

	return t.db.View(func(tx *bolt.Tx) error {

		b := tx.Bucket(bucketPrefix)
		if b == nil {
			return t.enumerateAllInTx(tx, prefix, seek, onlyKeys, cb)
		}

		return t.enumerateInBucket(newAppend(bucketPrefix, BucketSeparator), b, prefix, seek, onlyKeys, cb)

	})

}

func (t *implBoltStore) enumerateInBucket(bucketWithSeparator []byte, b *bolt.Bucket, prefix, seek []byte, onlyKeys bool, cb func(entry *store.RawEntry) bool) error {

	cur := b.Cursor()

	var k, v []byte
	if len(seek) > len(bucketWithSeparator) {
		k, v = cur.Seek(seek[len(bucketWithSeparator):])
	} else {
		k, v = cur.First()
	}

	for ; k != nil; k, v = cur.Next() {

		key := newAppend(bucketWithSeparator, k...)

		if !bytes.HasPrefix(key, prefix) {
			break
		}

		version, expiresAt, val, _ := store.DecodeEnvelope(v)
		if store.IsExpired(expiresAt) {
			continue
		}

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
			return nil
		}
	}

	return nil
}

func (t*implBoltStore) enumerateAll(prefix, seek []byte, onlyKeys bool, cb func(entry *store.RawEntry) bool) error {

	return t.db.View(func(tx *bolt.Tx) error {

		return t.enumerateAllInTx(tx, prefix, seek, onlyKeys, cb)

	})

}

func (t *implBoltStore) enumerateAllInTx(tx *bolt.Tx, prefix []byte, seek []byte, onlyKeys bool, cb func(entry *store.RawEntry) bool) error {
	return tx.ForEach(func(bucket []byte, b *bolt.Bucket) error {

		bucketWithSeparator := newAppend(bucket, BucketSeparator)
		n := len(bucketWithSeparator)
		p := prefix
		if len(p) > n {
			p = prefix[:n]
		}

		if !bytes.HasPrefix(bucketWithSeparator, p) {
			return nil
		}

		return t.enumerateInBucket(bucketWithSeparator, b, prefix, seek, onlyKeys, cb)

	})
}

func (t*implBoltStore) Compact(discardRatio float64) error {
	// bolt does not support compaction
	return nil
}

func (t*implBoltStore) Backup(w io.Writer, since uint64) (uint64, error) {

	var txId int

	err := t.db.View(func(tx *bolt.Tx) error {
		txId = tx.ID()
		_, err := tx.WriteTo(w)
		return err
	})

	return uint64(txId), err

}

func (t*implBoltStore) Restore(src io.Reader) error {

	dbPath := t.db.Path()
	if t.db.IsReadOnly() {
		return ErrDatabaseReadOnly
	}

	err := t.db.Close()
	if err != nil {
		return err
	}

	dst, err := os.OpenFile(dbPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, t.dataFilePerm)
	if err != nil {
		return err
	}

	_, err = io.Copy(dst, src)
	if err != nil {
		return err
	}

	opts := &bolt.Options{}
	for _, opt := range t.options {
		opt.apply(opts)
	}
	opts.ReadOnly = false

	t.db, err = bolt.Open(dbPath, t.dataFilePerm, opts)
	return err
}

func (t*implBoltStore) DropAll() error {

	dbPath := t.db.Path()
	if t.db.IsReadOnly() {
		return ErrDatabaseReadOnly
	}

	err := t.db.Close()
	if err != nil {
		return err
	}

	err = os.Remove(dbPath)
	if err != nil {
		return err
	}

	opts := &bolt.Options{}
	for _, opt := range t.options {
		opt.apply(opts)
	}

	t.db, err = bolt.Open(dbPath, t.dataFilePerm, opts)
	return err
}

func (t*implBoltStore) DropWithPrefix(prefix []byte) error {

	bucket, _ := t.parseKey(prefix)
	return t.db.Update(func(tx *bolt.Tx) error {

		b := tx.Bucket(bucket)
		if b == nil {
			return nil
		}

		return b.ForEach(func(k, v []byte) error {
			return b.Delete(k)
		})

	})

}

func (t*implBoltStore) Instance() interface{} {
	return t.db
}

func newAppend(arr []byte, other... byte) []byte {
	n := len(arr)
	m := len(other)
	result := make([]byte, n+m)
	copy(result, arr)
	copy(result[n:], other)
	return result
}
