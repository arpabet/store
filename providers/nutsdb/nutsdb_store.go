/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package nutsdbstore

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"github.com/nutsdb/nutsdb"
	pkgerrors "github.com/pkg/errors"
	"go.arpabet.com/store"
	"reflect"
	"sort"
)

var NutsdbStoreClass = reflect.TypeOf((*implNutsdbStore)(nil))

type implNutsdbStore struct {
	name string
	db   *nutsdb.DB
	hub  *store.WatchHub
}

func New(name string, dataDir string, options ...Option) (*implNutsdbStore, error) {
	if name == "" {
		return nil, pkgerrors.New("empty bean name")
	}
	db, err := OpenDatabase(dataDir, options...)
	if err != nil {
		return nil, err
	}
	return &implNutsdbStore{name: name, db: db, hub: store.NewWatchHub()}, nil
}

func FromDB(name string, db *nutsdb.DB) *implNutsdbStore {
	return &implNutsdbStore{name: name, db: db, hub: store.NewWatchHub()}
}

func (t *implNutsdbStore) Interface() store.ManagedTransactionalDataStore {
	return t
}

func (t *implNutsdbStore) BeanName() string {
	return t.name
}

func (t *implNutsdbStore) Destroy() error {
	return t.db.Close()
}

func (t *implNutsdbStore) Features() store.Capability {
	// native transactions (one writer at a time gives serialized RMW), native
	// ordered BTree iteration, native timer-wheel TTL (disk-level expiry, so no
	// sweeper), versioning via the envelope, atomic single-tx batches; watch is
	// served from an in-process hub.
	return store.TTLCapability | store.AtomicCapability | store.TransactionCapability |
		store.OrderedCapability | store.WatchCapability | store.BatchAtomicCapability
}

func (t *implNutsdbStore) Get(ctx context.Context) *store.GetOperation {
	return &store.GetOperation{DataStore: t, Context: ctx}
}

func (t *implNutsdbStore) Set(ctx context.Context) *store.SetOperation {
	return &store.SetOperation{DataStore: t, Context: ctx}
}

func (t *implNutsdbStore) CompareAndSet(ctx context.Context) *store.CompareAndSetOperation {
	return &store.CompareAndSetOperation{DataStore: t, Context: ctx}
}

func (t *implNutsdbStore) Increment(ctx context.Context) *store.IncrementOperation {
	return &store.IncrementOperation{DataStore: t, Context: ctx, Initial: 0, Delta: 1}
}

func (t *implNutsdbStore) Touch(ctx context.Context) *store.TouchOperation {
	return &store.TouchOperation{DataStore: t, Context: ctx}
}

func (t *implNutsdbStore) Remove(ctx context.Context) *store.RemoveOperation {
	return &store.RemoveOperation{DataStore: t, Context: ctx}
}

func (t *implNutsdbStore) Enumerate(ctx context.Context) *store.EnumerateOperation {
	return &store.EnumerateOperation{DataStore: t, Context: ctx}
}

// parseKey splits a full store key into (bucket, key) on the first separator.
// A key without a separator maps to a bucket with an empty key (mirrors the
// bolt provider); nutsdb rejects empty keys, so callers should always namespace
// keys as "bucket:key".
func parseKey(fullKey []byte) (string, []byte) {
	i := bytes.IndexByte(fullKey, BucketSeparator)
	if i == -1 {
		return string(fullKey), []byte{}
	}
	return string(fullKey[:i]), fullKey[i+1:]
}

// ttlToNuts converts the store's relative ttl seconds into the uint32 ttl nutsdb
// expects (0 means persistent). Negative/zero ttl is persistent.
func ttlToNuts(ttlSeconds int) uint32 {
	if ttlSeconds <= store.NoTTL {
		return 0
	}
	return uint32(ttlSeconds)
}

// readEnvelope fetches and decodes the stored envelope for (bucket, key),
// treating a missing bucket, a missing key, or an expired entry as absent.
// tx.Get sees writes pending in the same transaction, so this also reflects a
// bucket/key created earlier in the current transaction.
func readEnvelope(tx *nutsdb.Tx, bucket string, key []byte) (version, expiresAt int64, value []byte, found bool, err error) {
	raw, gerr := tx.Get(bucket, key)
	if gerr != nil {
		if errors.Is(gerr, nutsdb.ErrKeyNotFound) || isBucketNotFound(gerr) {
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

// put writes enc under (bucket, key), creating the bucket if needed and using
// nutsdb's native ttl so its timer wheel physically reclaims the entry on expiry.
func (sess *txSession) put(bucket string, key, enc []byte, ttlSeconds int) error {
	if err := sess.ensureBucket(bucket); err != nil {
		return err
	}
	return sess.tx.Put(bucket, key, enc, ttlToNuts(ttlSeconds))
}

func (t *implNutsdbStore) GetRaw(ctx context.Context, fullKey []byte, ttlPtr *int, versionPtr *int64, required bool) ([]byte, error) {
	bucket, key := parseKey(fullKey)
	var version, expiresAt int64
	var value []byte
	var found bool
	err := t.inRead(ctx, func(tx *nutsdb.Tx) error {
		var rerr error
		version, expiresAt, value, found, rerr = readEnvelope(tx, bucket, key)
		return rerr
	})
	if err != nil {
		return nil, wrapError(err)
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

func (t *implNutsdbStore) SetRaw(ctx context.Context, fullKey, value []byte, ttlSeconds int) error {
	bucket, key := parseKey(fullKey)
	var newVersion int64
	err := t.inWrite(ctx, func(sess *txSession) error {
		oldVersion, _, _, _, rerr := readEnvelope(sess.tx, bucket, key)
		if rerr != nil {
			return rerr
		}
		newVersion = oldVersion + 1
		enc := store.EncodeEnvelope(newVersion, store.ExpiryFromTtl(ttlSeconds), value)
		return sess.put(bucket, key, enc, ttlSeconds)
	})
	if err != nil {
		return wrapError(err)
	}
	t.emit(ctx, fullKey, value, store.WatchSet, newVersion)
	return nil
}

func (t *implNutsdbStore) CompareAndSetRaw(ctx context.Context, fullKey, value []byte, ttlSeconds int, version int64) (bool, error) {
	bucket, key := parseKey(fullKey)
	var updated bool
	var newVersion int64
	err := t.inWrite(ctx, func(sess *txSession) error {
		oldVersion, _, _, found, rerr := readEnvelope(sess.tx, bucket, key)
		if rerr != nil {
			return rerr
		}
		if found {
			if oldVersion != version {
				return nil
			}
		} else if version != 0 { // for a non-existent record the expected version is 0
			return nil
		}
		newVersion = oldVersion + 1
		enc := store.EncodeEnvelope(newVersion, store.ExpiryFromTtl(ttlSeconds), value)
		if perr := sess.put(bucket, key, enc, ttlSeconds); perr != nil {
			return perr
		}
		updated = true
		return nil
	})
	if err != nil {
		return false, wrapError(err)
	}
	if updated {
		t.emit(ctx, fullKey, value, store.WatchSet, newVersion)
	}
	return updated, nil
}

func (t *implNutsdbStore) IncrementRaw(ctx context.Context, fullKey []byte, initial, delta int64, ttlSeconds int) (prev int64, err error) {
	bucket, key := parseKey(fullKey)
	var newVersion int64
	var counter int64
	err = t.inWrite(ctx, func(sess *txSession) error {
		oldVersion, _, val, _, rerr := readEnvelope(sess.tx, bucket, key)
		if rerr != nil {
			return rerr
		}
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
		return sess.put(bucket, key, enc, ttlSeconds)
	})
	if err != nil {
		return prev, wrapError(err)
	}
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(counter))
	t.emit(ctx, fullKey, buf, store.WatchSet, newVersion)
	return prev, nil
}

func (t *implNutsdbStore) TouchRaw(ctx context.Context, fullKey []byte, ttlSeconds int) error {
	bucket, key := parseKey(fullKey)
	return wrapError(t.inWrite(ctx, func(sess *txSession) error {
		oldVersion, _, val, found, rerr := readEnvelope(sess.tx, bucket, key)
		if rerr != nil {
			return rerr
		}
		if !found {
			return nil
		}
		enc := store.EncodeEnvelope(oldVersion, store.ExpiryFromTtl(ttlSeconds), val)
		return sess.put(bucket, key, enc, ttlSeconds)
	}))
}

func (t *implNutsdbStore) RemoveRaw(ctx context.Context, fullKey []byte) error {
	bucket, key := parseKey(fullKey)
	err := t.inWrite(ctx, func(sess *txSession) error {
		if derr := sess.tx.Delete(bucket, key); derr != nil &&
			!errors.Is(derr, nutsdb.ErrKeyNotFound) && !isBucketNotFound(derr) {
			return derr
		}
		return nil
	})
	if err != nil {
		return wrapError(err)
	}
	t.emit(ctx, fullKey, nil, store.WatchDelete, 0)
	return nil
}

func (t *implNutsdbStore) EnumerateRaw(ctx context.Context, prefix, seek []byte, batchSize int, onlyKeys bool, reverse bool, cb func(entry *store.RawEntry) bool) error {
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

// ascend walks matching entries in ascending key order. With an empty prefix it
// sweeps every bucket (sorted by name); otherwise it scans the single bucket
// named by the prefix. The op layer applies the [start,end) upper bound and limit.
func (t *implNutsdbStore) ascend(ctx context.Context, prefix, seek []byte, onlyKeys bool, cb func(entry *store.RawEntry) bool) error {
	return t.inRead(ctx, func(tx *nutsdb.Tx) error {
		if len(prefix) == 0 {
			for _, bucket := range sortedBuckets(tx) {
				keys, vals, gerr := getBucketAll(tx, bucket)
				if gerr != nil {
					return gerr
				}
				stop, eerr := emitEntries(ctx, bucket, keys, vals, prefix, seek, onlyKeys, cb)
				if eerr != nil {
					return eerr
				}
				if stop {
					return nil
				}
			}
			return nil
		}

		bucketPrefix, keyPrefix := parseKey(prefix)
		keys, vals, serr := tx.PrefixScanEntries(bucketPrefix, keyPrefix, "", 0, nutsdb.ScanNoLimit, true, true)
		if serr != nil {
			if errors.Is(serr, nutsdb.ErrPrefixScan) || isBucketNotFound(serr) {
				return nil // no bucket / no keys match the prefix
			}
			return serr
		}
		_, eerr := emitEntries(ctx, bucketPrefix, keys, vals, prefix, seek, onlyKeys, cb)
		return eerr
	})
}

// emitEntries reconstructs the full "bucket:key" for each scanned entry, filters
// by prefix and seek, decodes the envelope (skipping expired entries), and
// delivers it to cb. It returns stop=true if cb asked to stop.
func emitEntries(ctx context.Context, bucket string, keys, vals [][]byte, prefix, seek []byte, onlyKeys bool, cb func(entry *store.RawEntry) bool) (stop bool, err error) {
	bucketWithSep := append([]byte(bucket), BucketSeparator)
	for i := range keys {
		if e := ctx.Err(); e != nil {
			return false, e
		}
		fullKey := concat(bucketWithSep, keys[i])
		if len(prefix) > 0 && !bytes.HasPrefix(fullKey, prefix) {
			continue
		}
		if bytes.Compare(fullKey, seek) < 0 {
			continue
		}
		version, expiresAt, val, _ := store.DecodeEnvelope(vals[i])
		if store.IsExpired(expiresAt) {
			continue
		}
		re := store.RawEntry{
			Key:     fullKey,
			Ttl:     store.TtlFromExpiry(expiresAt),
			Version: version,
		}
		if !onlyKeys {
			re.Value = cloneBytes(val)
		}
		if !cb(&re) {
			return true, nil
		}
	}
	return false, nil
}

// sortedBuckets returns every BTree bucket name in lexical order so a full
// enumeration (empty prefix) yields keys in overall sorted order.
func sortedBuckets(tx *nutsdb.Tx) []string {
	var names []string
	_ = tx.IterateBuckets(bucketDataStructure, "*", func(bucket string) bool {
		names = append(names, bucket)
		return true
	})
	sort.Strings(names)
	return names
}

// getBucketAll returns all keys and values of a bucket in key order, treating an
// empty bucket as no entries.
func getBucketAll(tx *nutsdb.Tx, bucket string) (keys, vals [][]byte, err error) {
	keys, vals, err = tx.GetAll(bucket)
	if err != nil {
		if errors.Is(err, nutsdb.ErrBucketEmpty) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	return keys, vals, nil
}

func (t *implNutsdbStore) Instance() interface{} {
	return t.db
}

func cloneBytes(b []byte) []byte {
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

// concat returns a fresh slice holding a followed by b.
func concat(a, b []byte) []byte {
	out := make([]byte, len(a)+len(b))
	copy(out, a)
	copy(out[len(a):], b)
	return out
}
