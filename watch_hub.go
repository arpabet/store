/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package store

import (
	"bytes"
	"context"
	"sync"
)

/**
WatchHub is an in-process publish/subscribe helper that backends without native
change feeds (bbolt, bolt, pebble, mem) embed to satisfy WatchRaw. Backends call
Notify after a committed mutation; WatchRaw delegates to Watch.

Delivery is best-effort: each subscriber has a bounded buffer and events are
dropped for a subscriber that is not keeping up, so a slow watcher never blocks
writers. This matches the at-most-once semantics typical of embedded watches.
*/

// WatchBufferSize is the per-subscriber channel buffer used by WatchHub.
var WatchBufferSize = 256

type watchSub struct {
	prefix []byte
	ch     chan *WatchEvent
}

type WatchHub struct {
	mu     sync.Mutex
	nextID int64
	subs   map[int64]*watchSub
}

func NewWatchHub() *WatchHub {
	return &WatchHub{subs: make(map[int64]*watchSub)}
}

// Notify fans an event out to every subscriber whose prefix matches the key.
func (t *WatchHub) Notify(event *WatchEvent) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, sub := range t.subs {
		if bytes.HasPrefix(event.Key, sub.prefix) {
			select {
			case sub.ch <- event:
			default: // drop if the subscriber is not draining fast enough
			}
		}
	}
}

func (t *WatchHub) subscribe(prefix []byte) (int64, *watchSub) {
	p := make([]byte, len(prefix))
	copy(p, prefix)
	sub := &watchSub{prefix: p, ch: make(chan *WatchEvent, WatchBufferSize)}
	t.mu.Lock()
	id := t.nextID
	t.nextID++
	t.subs[id] = sub
	t.mu.Unlock()
	return id, sub
}

func (t *WatchHub) unsubscribe(id int64) {
	t.mu.Lock()
	delete(t.subs, id)
	t.mu.Unlock()
}

// Watch blocks delivering matching events to cb until cb returns false or ctx
// is done. It is the WatchRaw implementation for hub-backed providers.
func (t *WatchHub) Watch(ctx context.Context, prefix []byte, cb func(*WatchEvent) bool) error {
	id, sub := t.subscribe(prefix)
	defer t.unsubscribe(id)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event := <-sub.ch:
			if !cb(event) {
				return nil
			}
		}
	}
}
