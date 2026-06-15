/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package nutsdbstore

import (
	"context"
	"errors"
	"github.com/nutsdb/nutsdb"
	"go.arpabet.com/store"
	"strings"
)

// isBucketNotFound reports whether err is nutsdb's "bucket not found". nutsdb
// constructs this error in a way that defeats errors.Is (several identically
// worded sentinels, returned unwrapped), so it is matched by message. Reads of a
// not-yet-created bucket surface this and are treated as "absent".
func isBucketNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "bucket not found")
}

// wrapError maps nutsdb's sentinel errors onto the store package's portable
// errors so callers can switch on store.Err* regardless of the backend.
func wrapError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return err
	case errors.Is(err, nutsdb.ErrKeyNotFound):
		return store.ErrNotFound
	case errors.Is(err, nutsdb.ErrKeyEmpty):
		return store.ErrEmptyKey
	case errors.Is(err, nutsdb.ErrTxNotWritable):
		return store.ErrReadOnlyTxn
	case errors.Is(err, nutsdb.ErrTxClosed), errors.Is(err, nutsdb.ErrCannotCommitAClosedTx),
		errors.Is(err, nutsdb.ErrCannotRollbackAClosedTx):
		return store.ErrDiscardedTxn
	case errors.Is(err, nutsdb.ErrTxnTooBig), errors.Is(err, nutsdb.ErrTxnExceedWriteLimit):
		return store.ErrTooBigTxn
	case errors.Is(err, nutsdb.ErrDBClosed):
		return store.ErrAlreadyClosed
	default:
		return err
	}
}
