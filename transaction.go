/*
 * Copyright (c) 2023 Zander Schwid & Co. LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package store

type implInnerTransaction struct {
	parent Transaction
}

func NewInnerTransaction(parent Transaction) Transaction {
	return &implInnerTransaction{parent: parent}
}

func (t *implInnerTransaction) ReadOnly() bool {
	return t.parent.ReadOnly()
}

func (t *implInnerTransaction) Commit() error {
	// parent will commit
	return nil
}

func (t *implInnerTransaction) Rollback() {
	// parent will rollback
}

func (t *implInnerTransaction) Instance() interface{} {
	// do operations on parent object
	return t.parent.Instance()
}




