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
	"sync"
	"time"
)

var CacheStoreClass = reflect.TypeOf((*cacheStore)(nil))

type cacheStore struct {
	name      string
	cache     *cache.Cache
	hub       *store.WatchHub

	// go-cache locks each operation, but read-modify-write (CAS, increment,
	// touch, versioned set) must be serialized across operations.
	mu sync.Mutex
}

func NewDefault(name string) *cacheStore {
	return New(name)
}

func New(name string, options ...Option) *cacheStore {
	cache := OpenDatabase(options...)
	return &cacheStore{name: name, cache: cache, hub: store.NewWatchHub()}
}

func FromCache(name string, c *cache.Cache) *cacheStore {
	return &cacheStore{name: name, cache: c, hub: store.NewWatchHub()}
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

func (t*cacheStore) Features() store.Capability {
	// go-cache stores items in an unordered map, so no OrderedCapability.
	return store.TTLCapability | store.AtomicCapability | store.WatchCapability
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

func (t*cacheStore) Watch(ctx context.Context) *store.WatchOperation {
	return &store.WatchOperation{DataStore: t, Context: ctx}
}

func (t*cacheStore) WatchRaw(ctx context.Context, prefix []byte, cb func(*store.WatchEvent) bool) error {
	return t.hub.Watch(ctx, prefix, cb)
}

func cacheTtl(ttlSeconds int) time.Duration {
	if ttlSeconds > 0 {
		return time.Second * time.Duration(ttlSeconds)
	}
	return cache.NoExpiration
}

// read decodes the envelope stored for key, treating expired entries as absent.
func (t*cacheStore) read(key string) (version, expiresAt int64, value []byte, found bool) {
	obj, ok := t.cache.Get(key)
	if !ok || obj == nil {
		return 0, 0, nil, false
	}
	raw, ok := obj.([]byte)
	if !ok {
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

func (t*cacheStore) GetRaw(ctx context.Context, key []byte, ttlPtr *int, versionPtr *int64, required bool) ([]byte, error) {

	version, expiresAt, val, found := t.read(string(key))
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

func (t*cacheStore) SetRaw(ctx context.Context, key, value []byte, ttlSeconds int) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	oldVersion, _, _, _ := t.read(string(key))
	newVersion := oldVersion + 1
	enc := store.EncodeEnvelope(newVersion, store.ExpiryFromTtl(ttlSeconds), value)
	t.cache.Set(string(key), enc, cacheTtl(ttlSeconds))
	t.notify(key, value, store.WatchSet, newVersion)
	return nil
}

func (t *cacheStore) IncrementRaw(ctx context.Context, key []byte, initial, delta int64, ttlSeconds int) (prev int64, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	oldVersion, _, val, _ := t.read(string(key))
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
	t.cache.Set(string(key), enc, cacheTtl(ttlSeconds))
	t.notify(key, buf, store.WatchSet, newVersion)
	return prev, nil
}

func (t*cacheStore) CompareAndSetRaw(ctx context.Context, key, value []byte, ttlSeconds int, version int64) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	oldVersion, _, _, found := t.read(string(key))
	if found {
		if oldVersion != version {
			return false, nil
		}
	} else if version != 0 { // for non-existent record the expected version is 0
		return false, nil
	}

	newVersion := oldVersion + 1
	enc := store.EncodeEnvelope(newVersion, store.ExpiryFromTtl(ttlSeconds), value)
	t.cache.Set(string(key), enc, cacheTtl(ttlSeconds))
	t.notify(key, value, store.WatchSet, newVersion)
	return true, nil
}

func (t *cacheStore) TouchRaw(ctx context.Context, key []byte, ttlSeconds int) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	oldVersion, _, val, found := t.read(string(key))
	if !found {
		return nil
	}
	enc := store.EncodeEnvelope(oldVersion, store.ExpiryFromTtl(ttlSeconds), val)
	t.cache.Set(string(key), enc, cacheTtl(ttlSeconds))
	return nil
}

func (t*cacheStore) RemoveRaw(ctx context.Context, key []byte) error {
	t.cache.Delete(string(key))
	t.notify(key, nil, store.WatchDelete, 0)
	return nil
}

func (t*cacheStore) notify(key, value []byte, eventType store.WatchEventType, version int64) {
	k := make([]byte, len(key))
	copy(k, key)
	var val []byte
	if value != nil {
		val = make([]byte, len(value))
		copy(val, value)
	}
	t.hub.Notify(&store.WatchEvent{Key: k, Value: val, Type: eventType, Version: version})
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

		raw, ok := item.Object.([]byte)
		if !ok || !strings.HasPrefix(key, prefixStr) || key < seekStr {
			continue
		}

		version, expiresAt, val, _ := store.DecodeEnvelope(raw)
		if store.IsExpired(expiresAt) {
			continue
		}

		re := store.RawEntry{
			Key:     []byte(key),
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

	for key := range t.cache.Items() {

		if strings.HasPrefix(key, prefixStr){
			t.cache.Delete(key)
		}

	}

	return nil

}

func (t*cacheStore) Instance() interface{} {
	return t.cache
}
