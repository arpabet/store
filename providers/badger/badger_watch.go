/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package badgerstore

import (
	"context"
	"errors"
	"github.com/dgraph-io/badger/v4"
	"github.com/dgraph-io/badger/v4/pb"
	"go.arpabet.com/store"
)

// badger entry meta bit marking a deletion, mirrors badger's internal bitDelete.
const bitDelete byte = 1 << 0

var errWatchStop = errors.New("watch stopped by callback")

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
			if len(kv.Meta) > 0 && kv.Meta[0]&bitDelete != 0 {
				event.Type = store.WatchDelete
			} else {
				event.Type = store.WatchSet
				event.Value = kv.Value
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
