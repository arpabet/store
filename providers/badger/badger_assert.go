/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package badgerstore

import "go.arpabet.com/store"

// compile-time proof that the full store contract is implemented
var (
	_ store.ManagedDataStore              = (*implBadgerStore)(nil)
	_ store.ManagedTransactionalDataStore = (*implBadgerStore)(nil)
	_ store.TransactionalDataStore        = (*implBadgerStore)(nil)
)
