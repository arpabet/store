/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package boltstore

import (
	"context"
	"github.com/boltdb/bolt"
	"go.arpabet.com/store"
)

// SweepExpired physically removes expired entries. It collects expired keys per
// bucket (mutating during ForEach is not allowed) and deletes them within the
// same write transaction, then emits WatchDelete for each after commit.
func (t *implBoltStore) SweepExpired(ctx context.Context) (int, error) {

	if t.db.IsReadOnly() {
		return 0, ErrDatabaseReadOnly
	}

	var expiredFull [][]byte
	err := t.db.Update(func(tx *bolt.Tx) error {
		return tx.ForEach(func(name []byte, b *bolt.Bucket) error {
			if err := ctx.Err(); err != nil {
				return err
			}

			var toDelete [][]byte
			err := b.ForEach(func(k, v []byte) error {
				if _, expiresAt, _, _ := store.DecodeEnvelope(v); store.IsExpired(expiresAt) {
					kc := make([]byte, len(k))
					copy(kc, k)
					toDelete = append(toDelete, kc)
				}
				return nil
			})
			if err != nil {
				return err
			}

			sep := newAppend(name, BucketSeparator)
			for _, k := range toDelete {
				if err := b.Delete(k); err != nil {
					return err
				}
				expiredFull = append(expiredFull, newAppend(sep, k...))
			}
			return nil
		})
	})
	if err != nil {
		return 0, err
	}

	for _, fullKey := range expiredFull {
		t.notify(fullKey, nil, store.WatchDelete, 0)
	}
	return len(expiredFull), nil
}
