/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package rosedbstore_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.arpabet.com/store"
	rosedbstore "go.arpabet.com/store/providers/rosedb"
)

// Regression test for the rosedb native-watch shutdown panic: rosedb's own
// Watch() feed goroutine is never stopped and panics with "send on closed
// channel" when Close races a pending event. The provider therefore keeps
// WatchQueueSize=0 and serves Watch from the hub; closing a store with an
// active watcher and in-flight writes must never panic.
func TestDestroyWithActiveWatcher(t *testing.T) {
	for i := 0; i < 5; i++ {
		s, err := rosedbstore.New("close", t.TempDir())
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		watcherDone := make(chan struct{})
		go func() {
			defer close(watcherDone)
			_ = s.Watch(ctx).ByPrefix("k:").Do(func(e *store.WatchEvent) bool { return true })
		}()

		for j := 0; j < 50; j++ {
			require.NoError(t, s.Set(context.Background()).ByKey("k:%d", j).String(fmt.Sprintf("v%d", j)))
		}

		// destroy while the watcher is still active and events are fresh
		require.NoError(t, s.Destroy())
		cancel()
		select {
		case <-watcherDone:
		case <-time.After(3 * time.Second):
			t.Fatal("watcher did not exit after cancel")
		}
	}
}

// Batch regression: duplicate keys inside one batch must version monotonically
// and the read path inside the batch must not deadlock against db.mu.
func TestBatchDuplicateKeyVersions(t *testing.T) {
	s, err := rosedbstore.New("batchdup", t.TempDir())
	require.NoError(t, err)
	defer s.Destroy()

	ctx := context.Background()
	require.NoError(t, s.Set(ctx).ByKey("d:1").String("v1")) // version 1

	require.NoError(t, s.Batch(ctx).
		Put([]byte("d:1"), []byte("v2"), store.NoTTL).
		Put([]byte("d:1"), []byte("v3"), store.NoTTL).
		Do())

	var ver int64
	val, err := s.Get(ctx).ByKey("d:1").WithVersion(&ver).ToString()
	require.NoError(t, err)
	require.Equal(t, "v3", val)
	require.Equal(t, int64(3), ver, "two batch writes over version 1 must land on 3")
}
