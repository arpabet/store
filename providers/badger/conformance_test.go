/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package badgerstore_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.arpabet.com/store"
	"go.arpabet.com/store/providers/badger"
	"go.arpabet.com/store/storetest"
)

func TestConformance(t *testing.T) {
	storetest.RunConformance(t, func(t *testing.T) store.ManagedDataStore {
		s, err := badgerstore.New("conf", t.TempDir(), badgerstore.WithLogger(false))
		require.NoError(t, err)
		t.Cleanup(func() { s.Destroy() })
		return s // *implBadgerStore implements ManagedDataStore (Interface() returns the transactional view)
	})
}
