/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package bboltstore

import (
	"errors"
	bolt "go.etcd.io/bbolt"
	"os"
	"time"
)

var (

	BucketSeparator = byte(':')

	ErrDatabaseReadOnly = errors.New("readonly")
	ErrInvalidSeek      = errors.New("invalid seek")
	ErrCanceled         = errors.New("operation was canceled")
)

// Option configures bolt using the functional options paradigm
// popularized by Rob Pike and Dave Cheney. If you're unfamiliar with this style,
// see https://commandcenter.blogspot.com/2014/01/self-referential-functions-and-design.html and
// https://dave.cheney.net/2014/10/17/functional-options-for-friendly-apis.
type Option interface {
	apply(*bolt.Options)
}

// OptionFunc implements Option interface.
type optionFunc func(*bolt.Options)

// apply the configuration to the provided config.
func (fn optionFunc) apply(r *bolt.Options) {
	fn(r)
}

// option that do nothing
func WithNope() Option {
	return optionFunc(func(opts *bolt.Options) {
	})
}

// Timeout is the amount of time to wait to obtain a file lock.
// When set to zero it will wait indefinitely. This option is only
// available on Darwin and Linux.
func WithTimeout(value time.Duration) Option {
	return optionFunc(func(opts *bolt.Options) {
		opts.Timeout = value
	})
}
func WithIndefinitelyTimeout(value time.Duration) Option {
	return optionFunc(func(opts *bolt.Options) {
		opts.Timeout = 0
	})
}

// Sets the DB.NoGrowSync flag before memory mapping the file.
func WithNoGrowSync() Option {
	return optionFunc(func(opts *bolt.Options) {
		opts.NoGrowSync = true
	})
}

// Do not sync freelist to disk. This improves the database write performance
// under normal operation, but requires a full database re-sync during recovery.
func WithNoFreelistSync() Option {
	return optionFunc(func(opts *bolt.Options) {
		opts.NoFreelistSync = true
	})
}

// FreelistType sets the backend freelist type. There are two options. Array which is simple but endures
// dramatic performance degradation if database is large and framentation in freelist is common.
// The alternative one is using hashmap, it is faster in almost all circumstances
// but it doesn't guarantee that it offers the smallest page id available. In normal case it is safe.
// The default type is array
func WithFreelistType(value bolt.FreelistType) Option {
	return optionFunc(func(opts *bolt.Options) {
		opts.NoFreelistSync = false
		opts.FreelistType = value
	})
}

// Open database in read-only mode. Uses flock(..., LOCK_SH |LOCK_NB) to
// grab a shared lock (UNIX).
func WithReadOnly() Option {
	return optionFunc(func(opts *bolt.Options) {
		opts.ReadOnly = true
	})
}

// Sets the DB.MmapFlags flag before memory mapping the file.
func WithMmapFlags(value int) Option {
	return optionFunc(func(opts *bolt.Options) {
		opts.MmapFlags = value
	})
}

// InitialMmapSize is the initial mmap size of the database
// in bytes. Read transactions won't block write transaction
// if the InitialMmapSize is large enough to hold database mmap
// size. (See DB.Begin for more information)
//
// If <=0, the initial map size is 0.
// If initialMmapSize is smaller than the previous database size,
// it takes no effect.
func WithInitialMmapSize(value int) Option {
	return optionFunc(func(opts *bolt.Options) {
		opts.InitialMmapSize = value
	})
}

// PageSize overrides the default OS page size.
func WithPageSize(value int) Option {
	return optionFunc(func(opts *bolt.Options) {
		opts.PageSize = value
	})
}

// NoSync sets the initial value of DB.NoSync. Normally this can just be
// set directly on the DB itself when returned from Open(), but this option
// is useful in APIs which expose Options but not the underlying DB.
func WithNoSync() Option {
	return optionFunc(func(opts *bolt.Options) {
		opts.NoSync = true
	})
}

// OpenFile is used to open files. It defaults to os.OpenFile. This option
// is useful for writing hermetic tests.
func WithOpenFileFunc(fn func(string, int, os.FileMode) (*os.File, error)) Option {
	return optionFunc(func(opts *bolt.Options) {
		opts.OpenFile = fn
	})
}

// Mlock locks database file in memory when set to true.
// It prevents potential page faults, however
// used memory can't be reclaimed. (UNIX only)
func WithMlock() Option {
	return optionFunc(func(opts *bolt.Options) {
		opts.Mlock = true
	})
}


