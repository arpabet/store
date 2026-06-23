/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package badgerstore

import (
	"context"
	"golang.org/x/xerrors"
	"github.com/dgraph-io/badger/v4"
	"github.com/dgraph-io/badger/v4/pb"
	"go.arpabet.com/store"
)

// valueUserMeta tags every entry this provider writes. Badger's Subscribe
// publishes only the entry's UserMeta (its internal delete bit is never
// exposed), so deletes are detected by the absence of this tag: txn.Delete
// produces an event with UserMeta 0.
const valueUserMeta byte = 0x1

var errWatchStop = xerrors.New("watch stopped by callback")

func (t *implBadgerStore) Features() store.Capability {
	caps := store.TTLCapability | store.AtomicCapability | store.TransactionCapability |
		store.OrderedCapability | store.WatchCapability
	if len(t.db.Opts().EncryptionKey) > 0 {
		caps |= store.EncryptedCapability
	}
	return caps
}

func (t *implBadgerStore) Watch(ctx context.Context) *store.WatchOperation {
	return &store.WatchOperation{DataStore: t, Context: ctx}
}

func (t *implBadgerStore) WatchRaw(ctx context.Context, prefix []byte, cb func(*store.WatchEvent) bool) error {
	matches := []pb.Match{{Prefix: prefix}}
	err := t.db.Subscribe(ctx, func(kvs *badger.KVList) error {
		for _, kv := range kvs.Kv {
			event := &store.WatchEvent{
				Key:     kv.Key,
				Version: int64(kv.Version),
			}
			if len(kv.Meta) > 0 && kv.Meta[0] == valueUserMeta {
				event.Type = store.WatchSet
				event.Value = kv.Value
			} else {
				event.Type = store.WatchDelete
			}
			if !cb(event) {
				return errWatchStop
			}
		}
		return nil
	}, matches)
	if err == errWatchStop || err == context.Canceled {
		return nil
	}
	return err
}
