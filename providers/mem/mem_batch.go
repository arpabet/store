/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package memstore

import (
	"context"
	"go.arpabet.com/store"
)

func (t *cacheStore) Batch(ctx context.Context) *store.BatchOperation {
	return &store.BatchOperation{DataStore: t, Context: ctx}
}

// SetBatchRaw applies entries under the write lock. It is not isolated from
// concurrent readers, so it does not advertise BatchAtomicCapability.
func (t *cacheStore) SetBatchRaw(ctx context.Context, entries []store.RawEntry) error {
	if len(entries) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	type notice struct {
		key, value []byte
		version    int64
	}
	notices := make([]notice, 0, len(entries))

	keys := make([][]byte, len(entries))
	for i := range entries {
		keys[i] = entries[i].Key
	}
	unlock := t.locks.LockMany(keys...)
	for i := range entries {
		e := &entries[i]
		oldVersion, _, _, _ := t.read(string(e.Key))
		newVersion := oldVersion + 1
		enc := store.EncodeEnvelope(newVersion, store.ExpiryFromTtl(e.Ttl), e.Value)
		t.cache.Set(string(e.Key), enc, cacheTtl(e.Ttl))
		notices = append(notices, notice{key: e.Key, value: e.Value, version: newVersion})
	}
	unlock()

	for _, n := range notices {
		t.notify(n.key, n.value, store.WatchSet, n.version)
	}
	return nil
}
