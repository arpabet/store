/*
 * Copyright (c) 2023 Zander Schwid & Co. LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package store

import (
	"errors"
	"os"
)

var (

	ErrNotFound = os.ErrNotExist

	// ErrInvalidRequest is returned if the user request is invalid.
	ErrInvalidRequest = errors.New("invalid request")

	// ErrConcurrentTransaction is returned when a transaction conflicts with another transaction.
	ErrConcurrentTxn = errors.New("concurrent transaction, try again")

	// ErrReadOnlyTxn is returned if an update function is called on a read-only transaction.
	ErrReadOnlyTxn = errors.New("read-only transaction has update operation")

	// ErrDiscardedTxn is returned if a previously discarded transaction is re-used.
	ErrDiscardedTxn = errors.New("transaction has been discarded")

	// ErrCanceledTxn is returned if user canceled transaction.
	ErrCanceledTxn = errors.New("transaction has been canceled")

	// ErrTooBigTxn is returned if too many writes are fit into a single transaction.
	ErrTooBigTxn = errors.New("transaction is too big")
	
	// ErrEmptyKey is returned if an empty key is passed on an update function.
	ErrEmptyKey = errors.New("empty key")

	// ErrInvalidKey is returned if the key has wrong character(s)
	ErrInvalidKey = errors.New("key is invalid")

	// ErrAlreadyClosed is returned when store is already closed
	ErrAlreadyClosed = errors.New("already closed")

	// ErrInternal
	ErrInternal = errors.New("internal error")

)
