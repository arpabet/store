/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package boltstore

import "go.arpabet.com/store"

// compile-time proof that the full store contract is implemented
var (
	_ store.ManagedDataStore = (*implBoltStore)(nil)
	_ store.Sweepable        = (*implBoltStore)(nil)
)
