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
		dataFile: dataFile,
		dataFilePerm: dataFilePerm,
		options: options,
	}, nil
}

func FromDB(name string, db *bolt.DB) *implBoltStore {
	return &implBoltStore{name: name, db: db}
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

func (t*implBoltStore) GetRaw(ctx context.Context, key []byte, ttlPtr *int, versionPtr *int64, required bool) ([]byte, error) {
	return t.getImpl(key, required)
}

func (t*implBoltStore) parseKey(fullKey []byte) ([]byte, []byte) {
	i := bytes.IndexByte(fullKey, BucketSeparator)
	if i == -1 {
		return fullKey, []byte{}
	} else {
		return fullKey[:i], fullKey[i+1:]
	}
}

func (t*implBoltStore) SetRaw(ctx context.Context, fullKey, value []byte, ttlSeconds int) error {

	if t.db.IsReadOnly() {
		return ErrDatabaseReadOnly
	}

	bucket, key := t.parseKey(fullKey)
	return t.db.Update(func(tx *bolt.Tx) error {

		b, err := tx.CreateBucketIfNotExists(bucket)
		if err != nil {
			return err
		}

		return b.Put(key, value)

	})

}

func (t *implBoltStore) IncrementRaw(ctx context.Context, key []byte, initial, delta int64, ttlSeconds int) (prev int64, err error) {
	err = t.UpdateRaw(ctx, key, func(entry *store.RawEntry) bool {
		counter := initial
		if len(entry.Value) >= 8 {
			counter = int64(binary.BigEndian.Uint64(entry.Value))
		}
		prev = counter
		counter += delta
		entry.Value = make([]byte, 8)
		binary.BigEndian.PutUint64(entry.Value, uint64(counter))
		entry.Ttl = ttlSeconds
		return true
	})
	return
}

func (t *implBoltStore) UpdateRaw(ctx context.Context, fullKey []byte, cb func(entry *store.RawEntry) bool) error {

	if t.db.IsReadOnly() {
		return ErrDatabaseReadOnly
	}

	bucket, key := t.parseKey(fullKey)
	return t.db.Update(func(tx *bolt.Tx) error {

		b, err := tx.CreateBucketIfNotExists(bucket)
		if err != nil {
			return err
		}

		re := store.RawEntry{
			Key:     key,
			Value:   b.Get(key),
			Ttl:     0,
			Version: 0,
		}

		if !cb(&re) {
			return ErrCanceled
		}

		return b.Put(key, re.Value)
	})
}

func (t*implBoltStore) CompareAndSetRaw(ctx context.Context, key, value []byte, ttlSeconds int, version int64) (bool, error) {
	return true, t.SetRaw(ctx, key, value, ttlSeconds)
}

func (t *implBoltStore) TouchRaw(ctx context.Context, fullKey []byte, ttlSeconds int) error {
	return nil
}

func (t*implBoltStore) RemoveRaw(ctx context.Context, fullKey []byte) error {

	if t.db.IsReadOnly() {
		return ErrDatabaseReadOnly
	}

	bucket, key := t.parseKey(fullKey)
	return t.db.Update(func(tx *bolt.Tx) error {

		b, err := tx.CreateBucketIfNotExists(bucket)
		if err != nil {
			return err
		}

		return b.Delete(key)
	})

}

func (t*implBoltStore) getImpl(fullKey []byte, required bool) ([]byte, error) {

	var val []byte

	bucket, key := t.parseKey(fullKey)
	err := t.db.View(func(tx *bolt.Tx) error {

		b := tx.Bucket(bucket)
		if b == nil {
			return nil
		}

		val = b.Get(key)
		return nil
	})

	if err != nil {
		return nil, err
	}

	if val == nil && required {
		return nil, os.ErrNotExist
	}

	return val, nil
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

		key := append(bucketWithSeparator, k...)

		if !bytes.HasPrefix(key, prefix) {
			break
		}

		re := store.RawEntry{
			Key:     key,
			Ttl:     0,
			Version: 0,
		}

		if !onlyKeys {
			re.Value = v
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