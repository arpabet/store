/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package pebblestore_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.arpabet.com/store"
	"go.arpabet.com/store/providers/pebble"
	"go.arpabet.com/store/storetest"
)

func TestConformance(t *testing.T) {
	storetest.RunConformance(t, func(t *testing.T) store.ManagedDataStore {
		s, err := pebblestore.New("conf", t.TempDir(), nil)
		require.NoError(t, err)
		t.Cleanup(func() { s.Destroy() })
		return s.Interface()
	})
}
