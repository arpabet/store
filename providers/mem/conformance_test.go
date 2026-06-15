/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package memstore_test

import (
	"testing"

	"go.arpabet.com/store"
	"go.arpabet.com/store/providers/mem"
	"go.arpabet.com/store/storetest"
)

func TestConformance(t *testing.T) {
	storetest.RunConformance(t, func(t *testing.T) store.ManagedDataStore {
		s := memstore.New("conf")
		t.Cleanup(func() { s.Destroy() })
		return s.Interface()
	})
}
