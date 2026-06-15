/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package nutsdbstore_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.arpabet.com/store"
	nutsdbstore "go.arpabet.com/store/providers/nutsdb"
)

// Batch regression: duplicate keys inside one batch must version monotonically
// because the read path inside the batch sees the batch's own pending writes.
func TestBatchDuplicateKeyVersions(t *testing.T) {
	s, err := nutsdbstore.New("batchdup", t.TempDir())
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

// A committed transaction makes all its writes visible atomically; a rolled-back
// transaction leaves no trace.
func TestTransactionCommitRollback(t *testing.T) {
	s, err := nutsdbstore.New("txn", t.TempDir())
	require.NoError(t, err)
	defer s.Destroy()

	// commit path
	ctx := s.BeginTransaction(context.Background(), false)
	require.NoError(t, s.Set(ctx).ByKey("t:a").String("alpha"))
	require.NoError(t, s.Set(ctx).ByKey("t:b").String("beta"))
	require.NoError(t, s.EndTransaction(ctx, nil))

	val, err := s.Get(context.Background()).ByKey("t:a").ToString()
	require.NoError(t, err)
	require.Equal(t, "alpha", val)

	// rollback path
	ctx = s.BeginTransaction(context.Background(), false)
	require.NoError(t, s.Set(ctx).ByKey("t:c").String("gamma"))
	// a non-nil errOps rolls back and is returned verbatim
	require.ErrorIs(t, s.EndTransaction(ctx, context.Canceled), context.Canceled)

	val, err = s.Get(context.Background()).ByKey("t:c").ToString()
	require.NoError(t, err)
	require.Equal(t, "", val, "rolled-back write must not be visible")
}

// Watch events for transactional writes are published only on commit, and
// dropped on rollback.
func TestTransactionWatchOnCommit(t *testing.T) {
	s, err := nutsdbstore.New("txnwatch", t.TempDir())
	require.NoError(t, err)
	defer s.Destroy()

	events := make(chan *store.WatchEvent, 16)
	wctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = s.Watch(wctx).ByPrefix("w:").Do(func(e *store.WatchEvent) bool {
			events <- e
			return true
		})
	}()
	time.Sleep(150 * time.Millisecond)

	// rolled-back transaction must emit nothing
	ctx := s.BeginTransaction(context.Background(), false)
	require.NoError(t, s.Set(ctx).ByKey("w:rollback").String("nope"))
	require.ErrorIs(t, s.EndTransaction(ctx, context.Canceled), context.Canceled)

	select {
	case e := <-events:
		t.Fatalf("rolled-back tx must not emit a watch event, got %s %q", e.Type, e.Key)
	case <-time.After(300 * time.Millisecond):
	}

	// committed transaction emits its event
	ctx = s.BeginTransaction(context.Background(), false)
	require.NoError(t, s.Set(ctx).ByKey("w:commit").String("yes"))
	require.NoError(t, s.EndTransaction(ctx, nil))

	select {
	case e := <-events:
		require.Equal(t, store.WatchSet, e.Type)
		require.Equal(t, "w:commit", string(e.Key))
	case <-time.After(3 * time.Second):
		t.Fatal("committed tx did not emit a watch event")
	}
}

// Backup/Restore round-trips values and versions across buckets.
func TestBackupRestore(t *testing.T) {
	src, err := nutsdbstore.New("src", t.TempDir())
	require.NoError(t, err)
	defer src.Destroy()

	ctx := context.Background()
	require.NoError(t, src.Set(ctx).ByKey("a:one").String("1"))
	require.NoError(t, src.Set(ctx).ByKey("a:one").String("1b")) // bump version to 2
	require.NoError(t, src.Set(ctx).ByKey("b:two").String("2"))

	var buf bytes.Buffer
	_, err = src.Backup(&buf, 0)
	require.NoError(t, err)

	dst, err := nutsdbstore.New("dst", t.TempDir())
	require.NoError(t, err)
	defer dst.Destroy()
	require.NoError(t, dst.Restore(&buf))

	var ver int64
	val, err := dst.Get(ctx).ByKey("a:one").WithVersion(&ver).ToString()
	require.NoError(t, err)
	require.Equal(t, "1b", val)
	require.Equal(t, int64(2), ver, "restore must preserve the envelope version")

	val, err = dst.Get(ctx).ByKey("b:two").ToString()
	require.NoError(t, err)
	require.Equal(t, "2", val)
}
