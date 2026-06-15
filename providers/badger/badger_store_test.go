/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package badgerstore_test

import (
	"context"
	"go.arpabet.com/store/providers/badger"
	"go.arpabet.com/store"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestBadgerStore(t *testing.T) {

	dir, err := os.MkdirTemp(os.TempDir(), "badgerstoretest")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	s, err := badgerstore.New("test", dir)
	require.NoError(t, err)
	defer s.Destroy()

	err = populateData(s)
	require.NoError(t, err)

	var visited bool
	var cnt int
	err = s.Enumerate(context.Background()).ByPrefix("test").Do(func(entry *store.RawEntry) bool {
		if string(entry.Key) == "test" && string(entry.Value) == "abc" {
			visited = true
		}
		cnt++
		return true
	})
	require.NoError(t, err)
	require.True(t, visited)
	require.Equal(t, 1, cnt)

	err = cleanData(s)
	require.NoError(t, err)
}

func populateData(store store.TransactionalDataStore) (err error) {

	ctx := store.BeginTransaction(context.Background(), false)
	defer func() {
		err = store.EndTransaction(ctx, err)
	}()

	err = store.Set(ctx).ByKey("test").String("abc")

	value, err := store.Get(ctx).ByKey("test").ToString()

	if value != "abc" {
		err = errors.Errorf("value not found")
	}

	return
}

func cleanData(store store.TransactionalDataStore) (err error) {

	ctx := store.BeginTransaction(context.Background(), false)
	defer func() {
		err = store.EndTransaction(ctx, err)
	}()

	err = store.Remove(ctx).ByKey("test").Do()

	return
}