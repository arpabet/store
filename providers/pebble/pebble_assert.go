/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package pebblestore

import "go.arpabet.com/store"

// compile-time proof that the full store contract is implemented
var (
	_ store.ManagedDataStore = (*implPebbleStore)(nil)
	_ store.Sweepable        = (*implPebbleStore)(nil)
)
