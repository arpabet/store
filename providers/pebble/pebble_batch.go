/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package pebblestore

import (
	"context"
	"go.arpabet.com/store"
)

func (t *implPebbleStore) Batch(ctx context.Context) *store.BatchOperation {
	return &store.BatchOperation{DataStore: t, Context: ctx}
}

// SetBatchRaw writes all entries in a single pebble Batch (atomic commit).
func (t *implPebbleStore) SetBatchRaw(ctx context.Context, entries []store.RawEntry) error {
	if len(entries) == 0 {
		return nil
	}

	keys := make([][]byte, len(entries))
	for i := range entries {
		keys[i] = entries[i].Key
	}
	unlock := t.locks.LockMany(keys...)
	defer unlock()

	batch := t.db.NewBatch()
	defer batch.Close()

	type notice struct {
		key, value []byte
		version    int64
	}
	notices := make([]notice, 0, len(entries))
	// pending batch writes are not visible via db.Get, so track versions assigned
	// in this batch so duplicate keys version monotonically.
	seen := make(map[string]int64, len(entries))

	for i := range entries {
		e := &entries[i]
		base, ok := seen[string(e.Key)]
		if !ok {
			oldVersion, _, _, _, err := t.readEnvelope(e.Key)
			if err != nil {
				return err
			}
			base = oldVersion
		}
		newVersion := base + 1
		seen[string(e.Key)] = newVersion
		enc := store.EncodeEnvelope(newVersion, store.ExpiryFromTtl(e.Ttl), e.Value)
		if err := batch.Set(e.Key, enc, WriteOptions); err != nil {
			return err
		}
		notices = append(notices, notice{key: e.Key, value: e.Value, version: newVersion})
	}

	// a cancelled context aborts before commit, so nothing is persisted (all-or-nothing)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := batch.Commit(WriteOptions); err != nil {
		return err
	}

	for _, n := range notices {
		t.notify(n.key, n.value, store.WatchSet, n.version)
	}
	return nil
}
