/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package rosedbstore

import (
	"github.com/rosedblabs/rosedb/v2"
	"reflect"
)

// Option configures the underlying rosedb.Options.
type Option func(*rosedb.Options)

func WithSync() Option {
	return func(o *rosedb.Options) { o.Sync = true }
}

func WithSegmentSize(size int64) Option {
	return func(o *rosedb.Options) { o.SegmentSize = size }
}

func WithBytesPerSync(value uint32) Option {
	return func(o *rosedb.Options) { o.BytesPerSync = value }
}

func OpenDatabase(dirPath string, options ...Option) (*rosedb.DB, error) {
	opts := rosedb.DefaultOptions
	opts.DirPath = dirPath
	// rosedb's native watch (WatchQueueSize>0) starts a sendEvent goroutine that
	// panics on Close ("send on closed channel"); we leave it disabled and serve
	// Watch from an in-process hub fed by our own writes instead.
	opts.WatchQueueSize = 0
	for _, opt := range options {
		opt(&opts)
	}
	return rosedb.Open(opts)
}

func ObjectType() reflect.Type {
	return RosedbStoreClass
}
