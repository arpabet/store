/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package store

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeSweeper is a DataStore (embedded nil; methods are never called) that also
// implements Sweepable so StartSweeper accepts it.
type fakeSweeper struct {
	DataStore
	mu    sync.Mutex
	calls int
}

func (f *fakeSweeper) SweepExpired(ctx context.Context) (int, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return 0, nil
}

func (f *fakeSweeper) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// plainStore implements DataStore but not Sweepable.
type plainStore struct{ DataStore }

func TestStartSweeperTicks(t *testing.T) {
	f := &fakeSweeper{}
	stop, err := StartSweeper(f, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	time.Sleep(120 * time.Millisecond)
	stop()
	if got := f.count(); got < 2 {
		t.Fatalf("expected at least 2 sweeps, got %d", got)
	}
	// stop must be idempotent and not call again after returning
	after := f.count()
	stop()
	time.Sleep(40 * time.Millisecond)
	if f.count() != after {
		t.Fatalf("sweeper kept running after stop")
	}
}

func TestStartSweeperNotSupported(t *testing.T) {
	if _, err := StartSweeper(plainStore{}, time.Second); err != ErrNotSupported {
		t.Fatalf("expected ErrNotSupported, got %v", err)
	}
}

func TestStartSweeperBadInterval(t *testing.T) {
	if _, err := StartSweeper(&fakeSweeper{}, 0); err != ErrInvalidRequest {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}
