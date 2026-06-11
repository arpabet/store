/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package memstore

import (
	"context"
	"encoding/binary"
	"go.arpabet.com/store"
	"io"
	"os"
	"github.com/patrickmn/go-cache"
	"reflect"
	"strings"
	"time"
)

var CacheStoreClass = reflect.TypeOf((*cacheStore)(nil))

type cacheStore struct {
	name      string
	cache     *cache.Cache
}

func NewDefault(name string) *cacheStore {
	return New(name)
}

func New(name string, options ...Option) *cacheStore {
	cache := OpenDatabase(options...)
	return &cacheStore{name: name, cache: cache}
}

func FromCache(name string, c *cache.Cache) *cacheStore {
	return &cacheStore{name: name, cache: c}
}

func (t*cacheStore) Interface() store.ManagedDataStore {
	return t
}

func (t*cacheStore) BeanName() string {
	return t.name
}

func (t*cacheStore) Destroy() error {
	return nil
}

func (t*cacheStore) Get(ctx context.Context) *store.GetOperation {
	return &store.GetOperation{DataStore: t, Context: ctx}
}

func (t*cacheStore) Set(ctx context.Context) *store.SetOperation {
	return &store.SetOperation{DataStore: t, Context: ctx}
}

func (t*cacheStore) CompareAndSet(ctx context.Context) *store.CompareAndSetOperation {
	return &store.CompareAndSetOperation{DataStore: t, Context: ctx}
}

func (t *cacheStore) Increment(ctx context.Context) *store.IncrementOperation {
	return &store.IncrementOperation{DataStore: t, Context: ctx, Initial: 0, Delta: 1}
}

func (t *cacheStore) Touch(ctx context.Context) *store.TouchOperation {
	return &store.TouchOperation{DataStore: t, Context: ctx}
}

func (t*cacheStore) Remove(ctx context.Context) *store.RemoveOperation {
	return &store.RemoveOperation{DataStore: t, Context: ctx}
}

func (t*cacheStore) Enumerate(ctx context.Context) *store.EnumerateOperation {
	return &store.EnumerateOperation{DataStore: t, Context: ctx}
}

func (t*cacheStore) GetRaw(ctx context.Context, key []byte, ttlPtr *int, versionPtr *int64, required bool) ([]byte, error) {
	return t.getImpl(key, required)
}

func (t*cacheStore) SetRaw(ctx context.Context, key, value []byte, ttlSeconds int) error {

	ttl := cache.NoExpiration
	if ttlSeconds > 0 {
		ttl = time.Second * time.Duration(ttlSeconds)
	}

	t.cache.Set(string(key), value, ttl)
	return nil
}

func (t *cacheStore) IncrementRaw(ctx context.Context, key []byte, initial, delta int64, ttlSeconds int) (prev int64, err error) {
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

func (t *cacheStore) UpdateRaw(ctx context.Context, key []byte, cb func(entry *store.RawEntry) bool) error {

	rawEntry := &store.RawEntry {
		Key: key,
		Ttl: store.NoTTL,
		Version: 0,
	}

	if obj, ok := t.cache.Get(string(key)); ok && obj != nil {
		if b, ok := obj.([]byte); ok {
			rawEntry.Value = b
		}
	}

	if !cb(rawEntry) {
		return ErrCanceled
	}

	ttl := cache.NoExpiration
	if rawEntry.Ttl > 0 {
		ttl = time.Second * time.Duration(rawEntry.Ttl)
	}

	t.cache.Set(string(key), rawEntry.Value, ttl)
	return nil
}

func (t*cacheStore) CompareAndSetRaw(ctx context.Context, key, value []byte, ttlSeconds int, version int64) (bool, error) {
	return true, t.SetRaw(ctx, key, value, ttlSeconds)
}

func (t *cacheStore) TouchRaw(ctx context.Context, key []byte, ttlSeconds int) error {

	var value []byte

	if obj, ok := t.cache.Get(string(key)); ok && obj != nil {
		if b, ok := obj.([]byte); ok {
			value = b
		}
	}

	ttl := cache.NoExpiration
	if ttlSeconds > 0 {
		ttl = time.Second * time.Duration(ttlSeconds)
	}

	t.cache.Set(string(key),value, ttl)
	return nil
}

func (t*cacheStore) RemoveRaw(ctx context.Context, key []byte) error {
	t.cache.Delete(string(key))
	return nil
}

func (t*cacheStore) getImpl(key []byte, required bool) ([]byte, error) {

	var val []byte
	if obj, ok := t.cache.Get(string(key)); ok && obj != nil {
		if b, ok := obj.([]byte); ok {
			val = b
		}
	}

	if val == nil && required {
		return nil, os.ErrNotExist
	}

	return val, nil
}

func (t*cacheStore) EnumerateRaw(ctx context.Context, prefix, seek []byte, batchSize int, onlyKeys bool, reverse bool, cb func(entry *store.RawEntry) bool) error {
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

func (t*cacheStore) doEnumerateRaw(prefix, seek []byte, batchSize int, onlyKeys bool, cb func(entry *store.RawEntry) bool) error {

	prefixStr := string(prefix)
	seekStr := string(seek)

	for key, item := range t.cache.Items() {

		if val, ok := item.Object.([]byte); ok && strings.HasPrefix(key, prefixStr) && key >= seekStr {
			re := store.RawEntry{
				Key:     []byte(key),
				Ttl:     int(item.Expiration),
				Version: item.Expiration,
			}
			if !onlyKeys {
				re.Value = val
			}
			if !cb(&re) {
				break
			}
		}

	}

	return nil
}

func (t*cacheStore) Compact(discardRatio float64) error {
	t.cache.DeleteExpired()
	return nil
}

func (t*cacheStore) Backup(w io.Writer, since uint64) (uint64, error) {
	return 0, t.cache.Save(w)
}

func (t*cacheStore) Restore(src io.Reader) error {
	return t.cache.Load(src)
}

func (t*cacheStore) DropAll() error {
	t.cache.Flush()
	return nil
}

func (t*cacheStore) DropWithPrefix(prefix []byte) error {

	prefixStr := string(prefix)

	for key, _ := range t.cache.Items() {

		if strings.HasPrefix(key, prefixStr){
			t.cache.Delete(key)
		}

	}

	return nil

}

func (t*cacheStore) Instance() interface{} {
	return t.cache
}
