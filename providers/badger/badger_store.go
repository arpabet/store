/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package badgerstore

import (
	"context"
	"encoding/binary"
	"go.arpabet.com/store"
	"github.com/dgraph-io/badger/v4"
	"github.com/pkg/errors"
	"io"
	"reflect"
	"time"
)

var BadgerStoreClass = reflect.TypeOf((*implBadgerStore)(nil))

type implBadgerStore struct {
	name string
	opts *StoreOptions
	db *badger.DB
}

func New(name string, dataDir string, options ...Option) (*implBadgerStore, error) {

	if name == "" {
		return nil, errors.New("empty bean name")
	}

	db, opts, err := OpenDatabase(dataDir, options...)
	if err != nil {
		return nil, wrapError(err)
	}

	return &implBadgerStore{name: name, opts: opts, db: db}, nil
}

func FromDB(name string, db *badger.DB) *implBadgerStore {
	return &implBadgerStore{name: name, opts: DefaultStoreOptions(), db: db}
}

func (t *implBadgerStore) Interface() store.ManagedTransactionalDataStore {
	return t
}

func (t *implBadgerStore) BeanName() string {
	return t.name
}

func (t *implBadgerStore) Destroy() error {
	return t.db.Close()
}

func (t *implBadgerStore) Get(ctx context.Context) *store.GetOperation {
	return &store.GetOperation{DataStore: t, Context: ctx}
}

func (t *implBadgerStore) Set(ctx context.Context) *store.SetOperation {
	return &store.SetOperation{DataStore: t, Context: ctx}
}

func (t *implBadgerStore) CompareAndSet(ctx context.Context) *store.CompareAndSetOperation {
	return &store.CompareAndSetOperation{DataStore: t, Context: ctx}
}

func (t *implBadgerStore) Increment(ctx context.Context) *store.IncrementOperation {
	return &store.IncrementOperation{DataStore: t, Context: ctx, Initial: 0, Delta: 1}
}

func (t *implBadgerStore) Touch(ctx context.Context) *store.TouchOperation {
	return &store.TouchOperation{DataStore: t, Context: ctx}
}

func (t *implBadgerStore) Remove(ctx context.Context) *store.RemoveOperation {
	return &store.RemoveOperation{DataStore: t, Context: ctx}
}

func (t *implBadgerStore) Enumerate(ctx context.Context) *store.EnumerateOperation {
	return &store.EnumerateOperation{DataStore: t, Context: ctx}
}

func (t *implBadgerStore) GetRaw(ctx context.Context, key []byte, ttlPtr *int, versionPtr *int64, required bool) ([]byte, error) {
	return t.getImpl(ctx, key, ttlPtr, versionPtr, required)
}

func (t *implBadgerStore) SetRaw(ctx context.Context, key, value []byte, ttlSeconds int) error {

	return t.doInTransaction(ctx, func(txn *badger.Txn) error {

		entry := &badger.Entry{Key: key, Value: value, UserMeta: valueUserMeta}

		if ttlSeconds > 0 {
			entry.ExpiresAt = uint64(time.Now().Unix() + int64(ttlSeconds))
		}

		return txn.SetEntry(entry)

	})

}

func (t *implBadgerStore) IncrementRaw(ctx context.Context, key []byte, initial, delta int64, ttlSeconds int) (prev int64, err error) {
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

func (t *implBadgerStore) UpdateRaw(ctx context.Context, key []byte, cb func(entry *store.RawEntry) bool) error {

	return t.doInTransaction(ctx, func(txn *badger.Txn) error {

		rawEntry := &store.RawEntry{
			Key:     key,
			Ttl:     store.NoTTL,
			Version: 0,
		}

		item, err := txn.Get(key)
		if err != nil {
			if err != badger.ErrKeyNotFound {
				return err
			}
		} else {
			rawEntry.Value, err = item.ValueCopy(nil)
			if err != nil {
				return err
			}
			rawEntry.Ttl = getTtl(item)
			rawEntry.Version = int64(item.Version())
		}

		if !cb(rawEntry) {
			return ErrTransactionCanceled
		}

		entry := &badger.Entry{
			Key:      key,
			Value:    rawEntry.Value,
			UserMeta: valueUserMeta}

		if rawEntry.Ttl > 0 {
			entry.ExpiresAt = uint64(time.Now().Unix() + int64(rawEntry.Ttl))
		}

		return txn.SetEntry(entry)
	})

}

func (t *implBadgerStore) CompareAndSetRaw(ctx context.Context, key, value []byte, ttlSeconds int, version int64) (bool, error) {

	var updated bool
	err := t.doInTransaction(ctx, func(txn *badger.Txn) error {

		entry := &badger.Entry{Key: key, Value: value, UserMeta: valueUserMeta}

		if ttlSeconds > 0 {
			entry.ExpiresAt = uint64(time.Now().Unix() + int64(ttlSeconds))
		}

		item, err := txn.Get(key)
		if err != nil {
			if err == badger.ErrKeyNotFound {
				if version != 0 { // for non exist item version is 0
					return nil
				}
			} else {
				return err
			}
		} else if item.Version() != uint64(version) {
			return nil
		}

		err = txn.SetEntry(entry)
		if err == nil {
			updated = true
		}

		return err
	})

	return updated, err

}

func (t *implBadgerStore) TouchRaw(ctx context.Context, key []byte, ttlSeconds int) error {

	return t.doInTransaction(ctx, func(txn *badger.Txn) error {

		var value []byte

		item, err := txn.Get(key)
		if err != nil {
			if err != badger.ErrKeyNotFound {
				return err
			}
		} else {
			value, err = item.ValueCopy(nil)
			if err != nil {
				return err
			}
		}

		entry := &badger.Entry{Key: key, Value: value, UserMeta: valueUserMeta}

		if ttlSeconds > 0 {
			entry.ExpiresAt = uint64(time.Now().Unix() + int64(ttlSeconds))
		}

		return txn.SetEntry(entry)

	})
}

func (t *implBadgerStore) RemoveRaw(ctx context.Context, key []byte) (err error) {

	return t.doInTransaction(ctx, func(txn *badger.Txn) error {
		return txn.Delete(key)
	})

}

func (t *implBadgerStore) doInTransaction(ctx context.Context, cb func(txn *badger.Txn) error) (err error) {

	var txn *badger.Txn
	var parentTx bool

	defer func() {
		if !parentTx && txn != nil {
			txn.Discard()
		}
	}()

	for i := 0; i < t.opts.ConcurrentRetryNum; i++ {

		err = ctx.Err()
		if err != nil {
			return err
		}

		txn, parentTx, err = t.getOrCreateTransaction(ctx, true)
		if err != nil {
			return err
		}

		err = cb(txn)
		if err != nil {
			return wrapError(err)
		}

		if !parentTx {
			err = txn.Commit()
			if err == badger.ErrConflict {
				txn = nil
				continue
			}
			if err != nil {
				return wrapError(err)
			}
		}

		return nil

	}

	return store.ErrConcurrentTxn

}

func (t *implBadgerStore) getImpl(ctx context.Context, key []byte, ttlPtr *int, versionPtr *int64, required bool) ([]byte, error) {

	txn, parentTx, err := t.getOrCreateTransaction(ctx, false)
	if err != nil {
		return nil, err
	}

	defer func() {
		if !parentTx {
			txn.Discard()
		}
	}()

	item, err := txn.Get(key)
	if err != nil {

		if err == badger.ErrKeyNotFound {
			if required {
				return nil, store.ErrNotFound
			} else {
				return nil, nil
			}
		} else {
			return nil, wrapError(err)
		}

	}

	err = ctx.Err()
	if err != nil {
		return nil, err
	}

	data, err := item.ValueCopy(nil)
	if err != nil {
		return nil, wrapError(err)
	}

	if ttlPtr != nil {
		*ttlPtr = getTtl(item)
	}

	if versionPtr != nil {
		*versionPtr = int64(item.Version())
	}

	return data, nil
}

func (t *implBadgerStore) EnumerateRaw(ctx context.Context, prefix, seek []byte, batchSize int, onlyKeys bool, reverse bool, cb func(entry *store.RawEntry) bool) error {
	// Reverse is served by collecting the ascending scan and replaying it in
	// reverse: badger's native reverse iterator seeks to the last key <= seek,
	// but the seek defaults to the prefix start (which sorts before every
	// prefixed key), so a native reverse prefix scan would find nothing. Caching
	// the ascending scan keeps the seek semantics identical to the forward path
	// and matches the other ordered providers.
	if reverse {
		var cache []*store.RawEntry
		if err := t.ascend(ctx, prefix, seek, batchSize, onlyKeys, func(e *store.RawEntry) bool {
			cache = append(cache, e)
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
	return t.ascend(ctx, prefix, seek, batchSize, onlyKeys, cb)
}

func (t *implBadgerStore) ascend(ctx context.Context, prefix, seek []byte, batchSize int, onlyKeys bool, cb func(entry *store.RawEntry) bool) error {

	txn, parentTx, err := t.getOrCreateTransaction(ctx, false)
	if err != nil {
		return err
	}

	defer func() {
		if !parentTx {
			txn.Discard()
		}
	}()

	options := badger.IteratorOptions{
		PrefetchValues: !onlyKeys,
		PrefetchSize:   batchSize,
		Reverse:        false,
		AllVersions:    false,
		Prefix:         prefix,
	}

	iter := txn.NewIterator(options)
	defer iter.Close()

	for iter.Seek(seek); iter.Valid(); iter.Next() {

		err = ctx.Err()
		if err != nil {
			return err
		}

		item := iter.Item()
		// copy the key: it aliases iterator-owned memory and must outlive the
		// iteration when the reverse path caches entries.
		key := item.KeyCopy(nil)
		var value []byte
		if !onlyKeys {
			value, err = item.ValueCopy(nil)
			if err != nil {
				return wrapError(err)
			}
		}
		rw := store.RawEntry{
			Key:     key,
			Value:   value,
			Ttl:     getTtl(item),
			Version: int64(item.Version()),
		}
		if !cb(&rw) {
			break
		}
	}

	return nil
}

func getTtl(item *badger.Item) int {
	expiresAt := item.ExpiresAt()
	if expiresAt == 0 {
		return 0
	}
	val := int(expiresAt - uint64(time.Now().Unix()))
	if val == 0 {
		val = -1
	}
	return val
}

func (t *implBadgerStore) Compact(discardRatio float64) error {
	return wrapError(t.db.RunValueLogGC(discardRatio))
}

func (t *implBadgerStore) Backup(w io.Writer, since uint64) (uint64, error) {
	newSince, err := t.db.Backup(w, since)
	return newSince, wrapError(err)
}

func (t *implBadgerStore) Restore(r io.Reader) error {
	return wrapError(t.db.Load(r, t.opts.MaxPendingWrites))
}

func (t *implBadgerStore) DropAll() error {
	return wrapError(t.db.DropAll())
}

func (t *implBadgerStore) DropWithPrefix(prefix []byte) error {
	return wrapError(t.db.DropPrefix(prefix))
}

func (t *implBadgerStore) Instance() interface{} {
	return t.db
}

func (t *implBadgerStore) BeginTransaction(ctx context.Context, readOnly bool) context.Context {
	if tx, ok := store.GetTransaction(ctx, t.name); ok {
		if readOnly || (readOnly == tx.ReadOnly()) {
			return store.WithTransaction(ctx, t.name, store.NewInnerTransaction(tx))
		}
	}
	tx := t.db.NewTransaction(!readOnly)
	return store.WithTransaction(ctx, t.name, NewTransaction(tx, readOnly))
}

func (t *implBadgerStore) EndTransaction(ctx context.Context, err error) error {
	if tx, ok := store.GetTransaction(ctx, t.name); ok {
		if err == nil {
			return tx.Commit()
		} else {
			tx.Rollback()
			return err
		}
	}
	return nil
}

func (t *implBadgerStore) getOrCreateTransaction(ctx context.Context, update bool) (*badger.Txn, bool, error) {
	readOnly := !update
	var txn *badger.Txn
	tx, parentTx := store.GetTransaction(ctx, t.name)
	if parentTx && (readOnly || (readOnly == tx.ReadOnly())) {
		var ok bool
		txn, ok = tx.Instance().(*badger.Txn)
		if !ok {
			return nil, false, ErrInvalidTransactionInContext
		}
	} else {
		txn = t.db.NewTransaction(update)
		parentTx = false
	}
	return txn, parentTx, nil
}