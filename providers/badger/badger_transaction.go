/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package badgerstore

import (
	"go.arpabet.com/store"
	"github.com/dgraph-io/badger/v4"
)

type implBadgerTransaction struct {
	tx *badger.Txn
	readOnly bool
}

func NewTransaction(tx *badger.Txn, readOnly bool) store.Transaction {
	return &implBadgerTransaction{tx: tx, readOnly: readOnly}
}

func (t *implBadgerTransaction) ReadOnly() bool {
	return t.readOnly
}

func (t *implBadgerTransaction) Commit() error {
	err := t.tx.Commit()
	if err != nil {
		return wrapError(err)
	}
	return nil
}

func (t *implBadgerTransaction) Rollback() {
	t.tx.Discard()
}

func (t *implBadgerTransaction) Instance() interface{} {
	return t.tx
}
