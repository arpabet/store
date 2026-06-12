/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package pebblestore

import (
	"context"
	"github.com/cockroachdb/pebble/v2"
	"go.arpabet.com/store"
)

// SweepExpired physically removes expired entries. It scans a consistent
// iterator to collect candidates, then re-checks each under the write lock
// before deleting, so a key that was rewritten with a fresh TTL between the scan
// and the delete is left intact. A WatchDelete is emitted for each removal.
func (t *implPebbleStore) SweepExpired(ctx context.Context) (int, error) {

	iter, err := t.db.NewIter(&pebble.IterOptions{})
	if err != nil {
		return 0, err
	}

	var candidates [][]byte
	for iter.First(); iter.Valid(); iter.Next() {
		if err := ctx.Err(); err != nil {
			iter.Close()
			return 0, err
		}
		if _, expiresAt, _, _ := store.DecodeEnvelope(iter.Value()); store.IsExpired(expiresAt) {
			k := make([]byte, len(iter.Key()))
			copy(k, iter.Key())
			candidates = append(candidates, k)
		}
	}
	if err := iter.Close(); err != nil {
		return 0, err
	}

	removed := 0
	for _, k := range candidates {
		if err := ctx.Err(); err != nil {
			return removed, err
		}

		t.locks.Lock(k)
		raw, closer, gerr := t.db.Get(k)
		if gerr == pebble.ErrNotFound {
			t.locks.Unlock(k)
			continue
		}
		if gerr != nil {
			t.locks.Unlock(k)
			return removed, gerr
		}
		_, expiresAt, _, _ := store.DecodeEnvelope(raw)
		closer.Close()
		if !store.IsExpired(expiresAt) { // rewritten with a fresh TTL meanwhile
			t.locks.Unlock(k)
			continue
		}
		if derr := t.db.Delete(k, WriteOptions); derr != nil {
			t.locks.Unlock(k)
			return removed, derr
		}
		removed++
		t.locks.Unlock(k)

		t.notify(k, nil, store.WatchDelete, 0)
	}

	return removed, nil
}
