/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package nutsdbstore

import (
	"context"
	"go.arpabet.com/store"
)

func (t *implNutsdbStore) Batch(ctx context.Context) *store.BatchOperation {
	return &store.BatchOperation{DataStore: t, Context: ctx}
}

// SetBatchRaw writes every entry inside a single nutsdb transaction, so a batch
// is all-or-nothing (BatchAtomicCapability). Versions are read through the same
// transaction, which sees its own pending writes, so duplicate keys within one
// batch version monotonically. A cancelled context rolls the whole batch back.
func (t *implNutsdbStore) SetBatchRaw(ctx context.Context, entries []store.RawEntry) error {
	if len(entries) == 0 {
		return nil
	}

	type notice struct {
		key, value []byte
		version    int64
	}
	notices := make([]notice, 0, len(entries))

	err := t.inWrite(ctx, func(sess *txSession) error {
		notices = notices[:0]
		for i := range entries {
			e := &entries[i]
			bucket, key := parseKey(e.Key)
			oldVersion, _, _, _, rerr := readEnvelope(sess.tx, bucket, key)
			if rerr != nil {
				return rerr
			}
			newVersion := oldVersion + 1
			enc := store.EncodeEnvelope(newVersion, store.ExpiryFromTtl(e.Ttl), e.Value)
			if perr := sess.put(bucket, key, enc, e.Ttl); perr != nil {
				return perr
			}
			notices = append(notices, notice{key: e.Key, value: e.Value, version: newVersion})
		}
		// a cancelled context rolls the whole transaction back (all-or-nothing)
		return ctx.Err()
	})
	if err != nil {
		return wrapError(err)
	}

	for _, n := range notices {
		t.emit(ctx, n.key, n.value, store.WatchSet, n.version)
	}
	return nil
}
