/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package boltstore_test

import (
	"context"
	"go.arpabet.com/store"
	"github.com/stretchr/testify/require"
	"go.arpabet.com/store/providers/bolt"
	"log"
	"os"
	"testing"
)

func TestPrimitives(t *testing.T) {

	file, err := os.CreateTemp(os.TempDir(), "boltdatabasetest.*.db")
	if err != nil {
		log.Fatal(err)
	}
	defer os.Remove(file.Name())

	s, err := boltstore.New("test", file.Name(), os.FileMode(0666))
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
	err = s.Enumerate(context.Background()).ByPrefix("%s:n", bucket).Do(func(entry *store.RawEntry) bool {
		require.Equal(t, "first:name", string(entry.Key))
		require.Equal(t, "value", string(entry.Value))
		cnt++
		return true
	})
	require.NoError(t, err)
	require.Equal(t, 1, cnt)

	cnt = 0
	err = s.Enumerate(context.Background()).ByPrefix("%s:name", bucket).Do(func(entry *store.RawEntry) bool {
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

}