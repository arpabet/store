/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package store

import (
	"fmt"
	"sync"
	"testing"
)

// Concurrent per-key counters protected by the striped mutex must not lose
// updates. Each slot is only written under its own key's stripe lock (a slice,
// not a map, so concurrent writes to different slots are safe).
func TestStripedMutexCounters(t *testing.T) {
	var sm StripedMutex

	const keys = 16
	const perKey = 200
	counters := make([]int, keys)

	var wg sync.WaitGroup
	for k := 0; k < keys; k++ {
		key := []byte(fmt.Sprintf("k%d", k))
		slot := k
		for i := 0; i < perKey; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sm.Lock(key)
				counters[slot]++
				sm.Unlock(key)
			}()
		}
	}
	wg.Wait()

	for k := 0; k < keys; k++ {
		if counters[k] != perKey {
			t.Fatalf("key k%d: got %d want %d", k, counters[k], perKey)
		}
	}
}

// LockMany must deduplicate stripes (same key twice, colliding keys) and not deadlock.
func TestStripedMutexLockMany(t *testing.T) {
	var sm StripedMutex
	k1 := []byte("alpha")
	k2 := []byte("beta")

	unlock := sm.LockMany(k1, k2, k1, k2)
	unlock()

	// reacquire to prove everything was released
	unlock = sm.LockAll()
	unlock()
}

// Two concurrent LockMany calls over overlapping key sets must not deadlock
// (ascending stripe order guarantees progress).
func TestStripedMutexNoDeadlock(t *testing.T) {
	var sm StripedMutex
	keysA := [][]byte{[]byte("a"), []byte("b"), []byte("c")}
	keysB := [][]byte{[]byte("c"), []byte("a"), []byte("d")}

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); u := sm.LockMany(keysA...); u() }()
		go func() { defer wg.Done(); u := sm.LockMany(keysB...); u() }()
	}
	wg.Wait()
}
