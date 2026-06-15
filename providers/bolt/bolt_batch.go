/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package boltstore

import (
	"context"
	"github.com/boltdb/bolt"
	"go.arpabet.com/store"
)

func (t *implBoltStore) Batch(ctx context.Context) *store.BatchOperation {
	return &store.BatchOperation{DataStore: t, Context: ctx}
}

// SetBatchRaw writes all entries in a single bolt transaction (all-or-nothing).
func (t *implBoltStore) SetBatchRaw(ctx context.Context, entries []store.RawEntry) error {
	if len(entries) == 0 {
		return nil
	}
	if t.db.IsReadOnly() {
		return ErrDatabaseReadOnly
	}

	type notice struct {
		fullKey, value []byte
		version        int64
	}
	notices := make([]notice, 0, len(entries))

	err := t.db.Update(func(tx *bolt.Tx) error {
		for i := range entries {
			e := &entries[i]
			bucket, key := t.parseKey(e.Key)
			b, err := tx.CreateBucketIfNotExists(bucket)
			if err != nil {
				return err
			}
			oldVersion, _, _, _ := readEnvelope(b, key)
			newVersion := oldVersion + 1
			enc := store.EncodeEnvelope(newVersion, store.ExpiryFromTtl(e.Ttl), e.Value)
			if err := b.Put(key, enc); err != nil {
				return err
			}
			notices = append(notices, notice{fullKey: e.Key, value: e.Value, version: newVersion})
		}
		// a cancelled context rolls the whole transaction back (all-or-nothing)
		return ctx.Err()
	})
	if err != nil {
		return err
	}

	for _, n := range notices {
		t.notify(n.fullKey, n.value, store.WatchSet, n.version)
	}
	return nil
}
