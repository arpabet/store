/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package nutsdbstore

import "go.arpabet.com/store"

// compile-time proof that the full store contract is implemented
var (
	_ store.ManagedDataStore              = (*implNutsdbStore)(nil)
	_ store.ManagedTransactionalDataStore = (*implNutsdbStore)(nil)
	_ store.TransactionalDataStore        = (*implNutsdbStore)(nil)
	_ store.Transaction                   = (*implNutsdbTransaction)(nil)
)
