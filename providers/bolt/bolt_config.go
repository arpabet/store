/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package boltstore

import (
	"golang.org/x/xerrors"
	"github.com/boltdb/bolt"
	"time"
)

var (

	BucketSeparator = byte(':')

	ErrDatabaseReadOnly = xerrors.New("readonly")
	ErrInvalidSeek      = xerrors.New("invalid seek")
	ErrCanceled         = xerrors.New("operation was canceled")
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