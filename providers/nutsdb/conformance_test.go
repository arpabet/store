/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package nutsdbstore_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.arpabet.com/store"
	nutsdbstore "go.arpabet.com/store/providers/nutsdb"
	"go.arpabet.com/store/storetest"
)

func TestConformance(t *testing.T) {
	storetest.RunConformance(t, func(t *testing.T) store.ManagedDataStore {
		s, err := nutsdbstore.New("conf", t.TempDir())
		require.NoError(t, err)
		t.Cleanup(func() { s.Destroy() })
		return s
	})
}
