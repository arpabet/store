/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package rosedbstore_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.arpabet.com/store"
	rosedbstore "go.arpabet.com/store/providers/rosedb"
	"go.arpabet.com/store/storetest"
)

func TestConformance(t *testing.T) {
	storetest.RunConformance(t, func(t *testing.T) store.ManagedDataStore {
		s, err := rosedbstore.New("conf", t.TempDir())
		require.NoError(t, err)
		t.Cleanup(func() { s.Destroy() })
		return s
	})
}
