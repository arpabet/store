/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package pebblestore

import (
	"github.com/cockroachdb/pebble/v2"
	"golang.org/x/xerrors"
)

var (

	WriteOptions = pebble.NoSync

	ErrInvalidFormat     = xerrors.New("invalid format")
	ErrOperationCanceled = xerrors.New("operation was canceled")
)

