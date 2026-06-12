/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package bboltstore_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.arpabet.com/store"
	"go.arpabet.com/store/providers/bbolt"
	"go.arpabet.com/store/storetest"
)

func TestConformance(t *testing.T) {
	storetest.RunConformance(t, func(t *testing.T) store.ManagedDataStore {
		file := filepath.Join(t.TempDir(), "conf.db")
		s, err := bboltstore.New("conf", file, os.FileMode(0666))
		require.NoError(t, err)
		t.Cleanup(func() { s.Destroy() })
		return s.Interface()
	})
}
