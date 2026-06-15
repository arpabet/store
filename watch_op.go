/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package store

import (
	"context"
	"fmt"
)

type WatchEventType int

const (
	// WatchSet indicates a key was created or updated.
	WatchSet WatchEventType = iota
	// WatchDelete indicates a key was removed (or expired).
	WatchDelete
)

func (t WatchEventType) String() string {
	switch t {
	case WatchSet:
		return "set"
	case WatchDelete:
		return "delete"
	default:
		return "unknown"
	}
}

// WatchEvent is delivered by WatchRaw for each change matching the watched prefix.
type WatchEvent struct {
	Key     []byte
	Value   []byte // nil for WatchDelete
	Type    WatchEventType
	Version int64
}

type WatchOperation struct {
	DataStore                 // should be initialized
	Context   context.Context // should be initialized
	prefix    []byte
}

func (t *WatchOperation) ByPrefix(formatPrefix string, args ...interface{}) *WatchOperation {
	if len(args) > 0 {
		t.prefix = []byte(fmt.Sprintf(formatPrefix, args...))
	} else {
		t.prefix = []byte(formatPrefix)
	}
	return t
}

func (t *WatchOperation) ByRawPrefix(prefix []byte) *WatchOperation {
	t.prefix = prefix
	return t
}

// Do blocks delivering events to cb until cb returns false or the context is
// cancelled. A nil/empty prefix watches every key.
func (t *WatchOperation) Do(cb func(*WatchEvent) bool) error {
	return t.DataStore.WatchRaw(t.Context, t.prefix, cb)
}
