/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package nutsdbstore

import (
	"bytes"
	"errors"
	"github.com/nutsdb/nutsdb"
	"go.arpabet.com/store"
	"io"
	"time"
)

// Compact merges nutsdb's data files to reclaim space from stale/expired
// records. nutsdb ignores the discard ratio. With fewer than two mergeable
// files there is nothing to do, which nutsdb reports as ErrDontNeedMerge — a
// no-op, not a failure.
func (t *implNutsdbStore) Compact(discardRatio float64) error {
	if err := t.db.Merge(); err != nil && !errors.Is(err, nutsdb.ErrDontNeedMerge) {
		return wrapError(err)
	}
	return nil
}

// Backup writes every live entry as length-prefixed (bucket, key, envelope)
// triples. The stored envelope is preserved verbatim so versions and expiry
// survive a round-trip through Restore. The since argument is ignored (full
// backup); the returned token is the backup time in unix seconds.
func (t *implNutsdbStore) Backup(w io.Writer, since uint64) (uint64, error) {
	err := t.db.View(func(tx *nutsdb.Tx) error {
		for _, bucket := range sortedBuckets(tx) {
			keys, vals, gerr := getBucketAll(tx, bucket)
			if gerr != nil {
				return gerr
			}
			bucketBytes := []byte(bucket)
			for i := range keys {
				if _, _, expiresAt, _ := decodeStored(vals[i]); store.IsExpired(expiresAt) {
					continue
				}
				if werr := writeBinary(w, bucketBytes); werr != nil {
					return werr
				}
				if werr := writeBinary(w, keys[i]); werr != nil {
					return werr
				}
				if werr := writeBinary(w, vals[i]); werr != nil {
					return werr
				}
			}
		}
		return nil
	})
	if err != nil {
		return 0, wrapError(err)
	}
	return uint64(time.Now().Unix()), nil
}

// Restore replaces all data with the contents of a Backup stream. Each entry is
// re-written with the native ttl recomputed from the envelope expiry so nutsdb's
// timer wheel still reclaims it; already-expired entries are dropped.
func (t *implNutsdbStore) Restore(r io.Reader) error {
	if err := t.DropAll(); err != nil {
		return err
	}
	br := newByteReader(r)
	return wrapError(t.db.Update(func(tx *nutsdb.Tx) error {
		sess := newTxSession(tx)
		for {
			bucket, err := readBinary(br)
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			key, err := readBinary(br)
			if err != nil {
				return err
			}
			value, err := readBinary(br)
			if err != nil {
				return err
			}
			_, _, expiresAt, _ := decodeStored(value)
			if store.IsExpired(expiresAt) {
				continue
			}
			if berr := sess.ensureBucket(string(bucket)); berr != nil {
				return berr
			}
			if perr := tx.Put(string(bucket), key, value, remainingTtl(expiresAt)); perr != nil {
				return perr
			}
		}
	}))
}

// DropAll deletes every bucket (and therefore every entry).
func (t *implNutsdbStore) DropAll() error {
	return wrapError(t.db.Update(func(tx *nutsdb.Tx) error {
		for _, bucket := range sortedBuckets(tx) {
			if err := tx.DeleteBucket(bucketDataStructure, bucket); err != nil {
				return err
			}
		}
		return nil
	}))
}

// DropWithPrefix deletes entries whose full key starts with prefix. A prefix of
// just "bucket:" drops the whole bucket; "bucket:keyPrefix" drops the matching
// keys within it.
func (t *implNutsdbStore) DropWithPrefix(prefix []byte) error {
	bucket, keyPrefix := parseKey(prefix)
	return wrapError(t.db.Update(func(tx *nutsdb.Tx) error {
		if !tx.ExistBucket(bucketDataStructure, bucket) {
			return nil
		}
		if len(keyPrefix) == 0 {
			return tx.DeleteBucket(bucketDataStructure, bucket)
		}
		keys, _, err := tx.PrefixScanEntries(bucket, keyPrefix, "", 0, nutsdb.ScanNoLimit, true, false)
		if err != nil {
			if errors.Is(err, nutsdb.ErrPrefixScan) {
				return nil
			}
			return err
		}
		for _, k := range keys {
			if !bytes.HasPrefix(k, keyPrefix) {
				continue
			}
			if derr := tx.Delete(bucket, k); derr != nil && !errors.Is(derr, nutsdb.ErrKeyNotFound) {
				return derr
			}
		}
		return nil
	}))
}

// decodeStored unwraps a stored envelope, returning its version and expiry.
func decodeStored(raw []byte) (value []byte, version, expiresAt int64, ok bool) {
	version, expiresAt, value, ok = store.DecodeEnvelope(raw)
	return value, version, expiresAt, ok
}

// remainingTtl converts an absolute expiry into the uint32 ttl nutsdb expects,
// clamped to at least one second so a not-yet-expired entry is not dropped.
func remainingTtl(expiresAt int64) uint32 {
	if expiresAt == 0 {
		return 0
	}
	remaining := expiresAt - time.Now().Unix()
	if remaining < 1 {
		remaining = 1
	}
	return uint32(remaining)
}
