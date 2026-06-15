/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package memstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.arpabet.com/store"
	memstore "go.arpabet.com/store/providers/mem"
)

// TTL expiry must surface as a WatchDelete event via ttlcache's eviction hook.
func TestExpiryEmitsWatchDelete(t *testing.T) {
	s := memstore.New("exp")
	defer s.Destroy()

	events := make(chan *store.WatchEvent, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = s.Watch(ctx).ByPrefix("k:").Do(func(e *store.WatchEvent) bool {
			events <- e
			return true
		})
	}()

	// give the watcher a moment to register, then write a 1s entry
	time.Sleep(150 * time.Millisecond)
	require.NoError(t, s.Set(context.Background()).ByKey("k:gone").WithTtl(1).String("x"))

	var sawSet, sawDelete bool
	deadline := time.After(6 * time.Second)
	for !sawDelete {
		select {
		case e := <-events:
			switch e.Type {
			case store.WatchSet:
				sawSet = true
			case store.WatchDelete:
				require.Equal(t, "k:gone", string(e.Key))
				sawDelete = true
			}
		case <-deadline:
			t.Fatalf("did not observe WatchDelete on expiry (sawSet=%v)", sawSet)
		}
	}
	require.True(t, sawSet, "should have seen the initial WatchSet too")
}
