/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package badgerstore

import (
	"context"
	"go.arpabet.com/store"
	"github.com/dgraph-io/badger/v4"
)

func wrapError(err error) error {
	switch err {
	case context.DeadlineExceeded:
		return err
	case context.Canceled:
		return err
	case badger.ErrConflict:
		return store.ErrConcurrentTxn
	case badger.ErrReadOnlyTxn:
		return store.ErrReadOnlyTxn
	case badger.ErrInvalidRequest:
		return store.ErrInvalidRequest
	case badger.ErrKeyNotFound:
		return store.ErrNotFound
	case badger.ErrEmptyKey:
		return store.ErrEmptyKey
	case badger.ErrInvalidKey:
		return store.ErrInvalidKey
	case badger.ErrDiscardedTxn:
		return store.ErrDiscardedTxn
	case badger.ErrTxnTooBig:
		return store.ErrTooBigTxn
	case badger.ErrDBClosed:
		return store.ErrAlreadyClosed
	case ErrTransactionCanceled:
		return store.ErrCanceledTxn
	default:
		return err
	}
}
