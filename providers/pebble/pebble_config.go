/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
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

