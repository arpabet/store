/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package otelstore_test

import (
	"testing"

	"go.arpabet.com/store"
	otelstore "go.arpabet.com/store/middleware/otel"
	memstore "go.arpabet.com/store/providers/mem"
	"go.arpabet.com/store/storetest"
)

func TestConformance(t *testing.T) {
	storetest.RunConformance(t, func(t *testing.T) store.ManagedDataStore {
		delegate := memstore.New("conf")
		t.Cleanup(func() { delegate.Destroy() })
		return otelstore.New(delegate.Interface())
	})
}
