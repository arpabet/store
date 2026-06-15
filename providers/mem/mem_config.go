/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package memstore

import (
	"errors"
	"time"
)

var (
	ErrCanceled         = errors.New("operation was canceled")
)

type Config struct {
	DefaultExpiration time.Duration
	// Deprecated: ttlcache schedules expiry against the nearest item deadline,
	// so an explicit cleanup interval is no longer used. Kept for API compatibility.
	CleanupInterval time.Duration
}

// Option configures memory storage using the functional options paradigm
// popularized by Rob Pike and Dave Cheney. If you're unfamiliar with this style,
// see https://commandcenter.blogspot.com/2014/01/self-referential-functions-and-design.html and
// https://dave.cheney.net/2014/10/17/functional-options-for-friendly-apis.
type Option interface {
	apply(*Config)
}

// OptionFunc implements Option interface.
type optionFunc func(*Config)

// apply the configuration to the provided config.
func (fn optionFunc) apply(r *Config) {
	fn(r)
}

// option that do nothing
func WithNope() Option {
	return optionFunc(func(opts *Config) {
	})
}

func WithDefaultExpiration(value time.Duration) Option {
	return optionFunc(func(opts *Config) {
		opts.DefaultExpiration = value
	})
}

func WithCleanupInterval(value time.Duration) Option {
	return optionFunc(func(opts *Config) {
		opts.CleanupInterval = value
	})
}
