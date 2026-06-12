/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package pebblestore

import (
	"github.com/cockroachdb/pebble/v2"
	"github.com/pkg/errors"
)

var (

	WriteOptions = pebble.NoSync

	ErrInvalidFormat     = errors.New("invalid format")
	ErrOperationCanceled = errors.New("operation was canceled")
)

