/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package memstore

import (
	"bufio"
	"context"
	"encoding/binary"
	"go.arpabet.com/store"
	"io"
	"github.com/jellydator/ttlcache/v3"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"
)

var CacheStoreClass = reflect.TypeOf((*cacheStore)(nil))

type cacheStore struct {
	name      string
	cache     *ttlcache.Cache[string, []byte]
	hub       *store.WatchHub

	// ttlcache locks each operation, but read-modify-write (CAS, increment,
	// touch, versioned set) must be serialized per key across operations.
	// Striped by key hash so writers to different keys proceed in parallel.
	locks store.StripedMutex
}

func NewDefault(name string) *cacheStore {
	return New(name)
}

func New(name string, options ...Option) *cacheStore {
	c := OpenDatabase(options...)
	t := &cacheStore{name: name, cache: c, hub: store.NewWatchHub()}
	t.registerEviction()
	go c.Start() // background expiry; safe to call again (no-op if already running)
	return t
}

// FromCache adopts an externally created cache. It takes ownership: do not call
// Start/Stop on the cache yourself.
func FromCache(name string, c *ttlcache.Cache[string, []byte]) *cacheStore {
	t := &cacheStore{name: name, cache: c, hub: store.NewWatchHub()}
	t.registerEviction()
	go c.Start()
	return t
}

// registerEviction turns native expiry into WatchDelete events. Explicit deletes
// (EvictionReasonDeleted) are notified by RemoveRaw, so only expirations are
// handled here to avoid double notification.
func (t *cacheStore) registerEviction() {
	t.cache.OnEviction(func(_ context.Context, reason ttlcache.EvictionReason, item *ttlcache.Item[string, []byte]) {
		if reason == ttlcache.EvictionReasonExpired {
			t.notify([]byte(item.Key()), nil, store.WatchDelete, 0)
		}
	})
}

func (t*cacheStore) Interface() store.ManagedDataStore {
	return t
}

func (t*cacheStore) BeanName() string {
	return t.name
}

func (t*cacheStore) Destroy() error {
	t.cache.Stop()
	return nil
}

func (t*cacheStore) Features() store.Capability {
	// enumeration sorts keys, so order / range / pagination are supported.
	return store.TTLCapability | store.AtomicCapability | store.WatchCapability | store.OrderedCapability
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
	return ttlcache.NoTTL
}

// read decodes the envelope stored for key, treating expired entries as absent.
func (t*cacheStore) read(key string) (version, expiresAt int64, value []byte, found bool) {
	item := t.cache.Get(key)
	if item == nil {
		return 0, 0, nil, false
	}
	v, exp, val, _ := store.DecodeEnvelope(item.Value())
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
	t.locks.Lock(key)
	oldVersion, _, _, _ := t.read(string(key))
	newVersion := oldVersion + 1
	enc := store.EncodeEnvelope(newVersion, store.ExpiryFromTtl(ttlSeconds), value)
	t.cache.Set(string(key), enc, cacheTtl(ttlSeconds))
	t.locks.Unlock(key)

	t.notify(key, value, store.WatchSet, newVersion)
	return nil
}

func (t *cacheStore) IncrementRaw(ctx context.Context, key []byte, initial, delta int64, ttlSeconds int) (prev int64, err error) {
	t.locks.Lock(key)
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
	t.locks.Unlock(key)

	t.notify(key, buf, store.WatchSet, newVersion)
	return prev, nil
}

func (t*cacheStore) CompareAndSetRaw(ctx context.Context, key, value []byte, ttlSeconds int, version int64) (bool, error) {
	t.locks.Lock(key)
	oldVersion, _, _, found := t.read(string(key))
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
	t.cache.Set(string(key), enc, cacheTtl(ttlSeconds))
	t.locks.Unlock(key)

	t.notify(key, value, store.WatchSet, newVersion)
	return true, nil
}

func (t *cacheStore) TouchRaw(ctx context.Context, key []byte, ttlSeconds int) error {
	t.locks.Lock(key)
	defer t.locks.Unlock(key)

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

	// ttlcache.Items() is an unordered map; sort keys so enumeration is ordered
	// (enables Seek/Range/pagination semantics and OrderedCapability).
	items := t.cache.Items()
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		item := items[key]

		if !strings.HasPrefix(key, prefixStr) || key < seekStr {
			continue
		}

		version, expiresAt, val, _ := store.DecodeEnvelope(item.Value())
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
	for _, item := range t.cache.Items() {
		raw := item.Value()
		if _, expiresAt, _, _ := store.DecodeEnvelope(raw); store.IsExpired(expiresAt) {
			continue
		}
		if err := writeBinary(w, []byte(item.Key())); err != nil {
			return 0, err
		}
		if err := writeBinary(w, raw); err != nil {
			return 0, err
		}
	}
	return 0, nil
}

func (t*cacheStore) Restore(src io.Reader) error {

	if err := t.DropAll(); err != nil {
		return err
	}

	br := bufio.NewReader(src)

	readBinary := func() ([]byte, error) {
		size, err := binary.ReadUvarint(br)
		if err != nil {
			return nil, err
		}
		buf := make([]byte, size)
		if _, err := io.ReadFull(br, buf); err != nil {
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
		val, err := readBinary()
		if err != nil {
			return err
		}
		_, expiresAt, _, _ := store.DecodeEnvelope(val)
		if store.IsExpired(expiresAt) {
			continue
		}
		t.cache.Set(string(key), val, cacheTtlFromExpiry(expiresAt))
	}

	return nil
}

// cacheTtlFromExpiry converts an absolute envelope expiry to a ttlcache duration.
func cacheTtlFromExpiry(expiresAtUnix int64) time.Duration {
	if expiresAtUnix == 0 {
		return ttlcache.NoTTL
	}
	remaining := expiresAtUnix - time.Now().Unix()
	if remaining < 1 {
		remaining = 1
	}
	return time.Duration(remaining) * time.Second
}

func (t*cacheStore) DropAll() error {
	t.cache.DeleteAll()
	return nil
}

func (t*cacheStore) DropWithPrefix(prefix []byte) error {

	prefixStr := string(prefix)

	for _, key := range t.cache.Keys() {
		if strings.HasPrefix(key, prefixStr) {
			t.cache.Delete(key)
		}
	}

	return nil
}

func (t*cacheStore) Instance() interface{} {
	return t.cache
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
