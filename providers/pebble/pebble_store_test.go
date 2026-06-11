/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package pebblestore_test

import (
	"bytes"
	"context"
	"github.com/stretchr/testify/require"
	"go.arpabet.com/store/providers/pebble"
	"go.arpabet.com/store"
	"os"
	"testing"
)

func TestPrimitives(t *testing.T) {

	dir, err := os.MkdirTemp(os.TempDir(), "pebblestoretest")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	s, err := pebblestore.New("test", dir, nil)
	require.NoError(t, err)

	defer s.Destroy()

	bucket := "first"

	err = s.Set(context.Background()).ByKey("%s:name", bucket).String("value")
	require.NoError(t, err)

	value, err := s.Get(context.Background()).ByKey("%s:name", bucket).ToString()
	require.NoError(t, err)

	require.Equal(t,"value", value)

	cnt := 0
	err = s.Enumerate(context.Background()).Do(func(entry *store.RawEntry) bool {
		require.Equal(t, "first:name", string(entry.Key))
		require.Equal(t, "value", string(entry.Value))
		cnt++
		return true
	})
	require.NoError(t, err)
	require.Equal(t, 1, cnt)

	cnt = 0
	err = s.Enumerate(context.Background()).ByPrefix("%s:", bucket).Do(func(entry *store.RawEntry) bool {
		require.Equal(t, "first:name", string(entry.Key))
		require.Equal(t, "value", string(entry.Value))
		cnt++
		return true
	})
	require.NoError(t, err)
	require.Equal(t, 1, cnt)

	cnt = 0
	err = s.Enumerate(context.Background()).ByPrefix("%s:n", bucket).Seek("%s:name", bucket).Do(func(entry *store.RawEntry) bool {
		require.Equal(t, "first:name", string(entry.Key))
		require.Equal(t, "value", string(entry.Value))
		cnt++
		return true
	})
	require.NoError(t, err)
	require.Equal(t, 1, cnt)

	cnt = 0
	err = s.Enumerate(context.Background()).ByPrefix("%s:nothing", bucket).Do(func(entry *store.RawEntry) bool {
		cnt++
		return true
	})
	require.NoError(t, err)
	require.Equal(t, 0, cnt)

	missing, err := s.Get(context.Background()).ByKey("%s:missing", bucket).ToString()
	require.NoError(t, err)
	require.Equal(t, "", missing)

	prev, err := s.Increment(context.Background()).ByKey("%s:counter", bucket).Do()
	require.NoError(t, err)
	require.Equal(t, int64(0), prev)

	prev, err = s.Increment(context.Background()).ByKey("%s:counter", bucket).Do()
	require.NoError(t, err)
	require.Equal(t, int64(1), prev)

	err = s.Remove(context.Background()).ByKey("%s:counter", bucket).Do()
	require.NoError(t, err)

	var backup bytes.Buffer
	_, err = s.Backup(&backup, 0)
	require.NoError(t, err)

	err = s.Restore(&backup)
	require.NoError(t, err)

	value, err = s.Get(context.Background()).ByKey("%s:name", bucket).ToString()
	require.NoError(t, err)
	require.Equal(t, "value", value)
}
