/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package nutsdbstore

import (
	"context"
	"go.arpabet.com/store"
)

func (t *implNutsdbStore) Watch(ctx context.Context) *store.WatchOperation {
	return &store.WatchOperation{DataStore: t, Context: ctx}
}

// WatchRaw serves change notifications from an in-process hub fed by this
// provider's own writes. nutsdb does have a native watch, but it targets a
// single (bucket, key) pair rather than a key prefix and must be explicitly
// enabled; the hub matches the prefix semantics the store contract expects and
// the behavior of the other embedded providers (bolt, bbolt, pebble, rosedb).
//
// Events for writes made inside an explicit transaction are published only once
// that transaction commits (see emit / implNutsdbTransaction).
func (t *implNutsdbStore) WatchRaw(ctx context.Context, prefix []byte, cb func(*store.WatchEvent) bool) error {
	return t.hub.Watch(ctx, prefix, cb)
}
