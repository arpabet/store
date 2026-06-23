/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package store

import (
	"golang.org/x/xerrors"
	"os"
)

var (

	ErrNotFound = os.ErrNotExist

	// ErrInvalidRequest is returned if the user request is invalid.
	ErrInvalidRequest = xerrors.New("invalid request")

	// ErrConcurrentTransaction is returned when a transaction conflicts with another transaction.
	ErrConcurrentTxn = xerrors.New("concurrent transaction, try again")

	// ErrReadOnlyTxn is returned if an update function is called on a read-only transaction.
	ErrReadOnlyTxn = xerrors.New("read-only transaction has update operation")

	// ErrDiscardedTxn is returned if a previously discarded transaction is re-used.
	ErrDiscardedTxn = xerrors.New("transaction has been discarded")

	// ErrCanceledTxn is returned if user canceled transaction.
	ErrCanceledTxn = xerrors.New("transaction has been canceled")

	// ErrTooBigTxn is returned if too many writes are fit into a single transaction.
	ErrTooBigTxn = xerrors.New("transaction is too big")

	// ErrEmptyKey is returned if an empty key is passed on an update function.
	ErrEmptyKey = xerrors.New("empty key")

	// ErrInvalidKey is returned if the key has wrong character(s)
	ErrInvalidKey = xerrors.New("key is invalid")

	// ErrAlreadyClosed is returned when store is already closed
	ErrAlreadyClosed = xerrors.New("already closed")

	// ErrInternal
	ErrInternal = xerrors.New("internal error")

	// ErrNotSupported is returned when an operation is not supported by the backend capabilities.
	ErrNotSupported = xerrors.New("operation not supported by this store")

)
