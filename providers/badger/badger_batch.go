/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package badgerstore

import (
	"context"
	"github.com/dgraph-io/badger/v4"
	"go.arpabet.com/store"
	"time"
)

func (t *implBadgerStore) Batch(ctx context.Context) *store.BatchOperation {
	return &store.BatchOperation{DataStore: t, Context: ctx}
}

// SetBatchRaw uses badger's WriteBatch, which flushes in chunks and is therefore
// not all-or-nothing (no BatchAtomicCapability). Notifications are delivered
// natively via Subscribe.
func (t *implBadgerStore) SetBatchRaw(ctx context.Context, entries []store.RawEntry) error {
	if len(entries) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	wb := t.db.NewWriteBatch()
	defer wb.Cancel()

	for i := range entries {
		e := &entries[i]
		entry := &badger.Entry{Key: e.Key, Value: e.Value, UserMeta: valueUserMeta}
		if e.Ttl > 0 {
			entry.ExpiresAt = uint64(time.Now().Unix() + int64(e.Ttl))
		}
		if err := wb.SetEntry(entry); err != nil {
			return wrapError(err)
		}
	}
	return wrapError(wb.Flush())
}
