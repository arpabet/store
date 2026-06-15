/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package store

import (
	"context"
	"reflect"
	"sync"
	"time"
)

/**
Sweepable is implemented by stores that hide expired entries on read but keep
them on disk until a sweep physically removes them — the envelope-backed engines
(bbolt, bolt, pebble). Stores with native expiry (Badger value-log GC, mem's
ttlcache janitor) reclaim space themselves and do not implement this.

A generic enumerate-based sweep is impossible because EnumerateRaw already skips
expired entries; only the provider can see and delete them.
*/

var SweepableClass = reflect.TypeOf((*Sweepable)(nil)).Elem()
type Sweepable interface {

	/**
	Physically deletes every entry whose TTL has elapsed, emitting a WatchDelete
	for each, and returns how many were removed. Honors ctx cancellation.
	*/

	SweepExpired(ctx context.Context) (removed int, err error)
}

/**
StartSweeper launches a background goroutine that calls SweepExpired on ds every
interval until the returned stop function is called (stop blocks until the
goroutine has finished). It returns ErrNotSupported if ds does not implement
Sweepable, or ErrInvalidRequest if interval is not positive.

Sweep errors are delivered to the optional onError callback; otherwise ignored.
*/

func StartSweeper(ds DataStore, interval time.Duration, onError ...func(error)) (stop func(), err error) {

	sw, ok := ds.(Sweepable)
	if !ok {
		return nil, ErrNotSupported
	}
	if interval <= 0 {
		return nil, ErrInvalidRequest
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, e := sw.SweepExpired(ctx); e != nil && e != context.Canceled {
					for _, h := range onError {
						h(e)
					}
				}
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(cancel)
		<-done
	}, nil
}
