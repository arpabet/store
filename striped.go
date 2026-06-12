/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package store

import (
	"sort"
	"sync"
)

/**
StripedMutex provides per-key write serialization for providers that implement
read-modify-write operations (versioned set, CAS, increment, touch) outside a
native transaction. Locking is by key hash across a fixed number of stripes, so
writers to different keys proceed in parallel while writers to the same key are
serialized; a single store-wide mutex would make every store single-writer.

Multi-key operations (batches, drops) lock their distinct stripes in ascending
order, which makes concurrent multi-key lockers deadlock-free.
*/

const stripedMutexSize = 64 // power of two

type StripedMutex struct {
	mus [stripedMutexSize]sync.Mutex
}

// fnv-1a over the key, masked to a stripe index.
func stripeIndex(key []byte) int {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for _, b := range key {
		h ^= uint64(b)
		h *= prime64
	}
	return int(h & (stripedMutexSize - 1))
}

// Lock locks the stripe owning key.
func (t *StripedMutex) Lock(key []byte) {
	t.mus[stripeIndex(key)].Lock()
}

// Unlock unlocks the stripe owning key.
func (t *StripedMutex) Unlock(key []byte) {
	t.mus[stripeIndex(key)].Unlock()
}

// LockMany locks the distinct stripes owning the given keys in ascending order
// and returns the unlock function (release in reverse order).
func (t *StripedMutex) LockMany(keys ...[]byte) (unlock func()) {
	return t.lockStripes(t.stripesFor(keys))
}

// LockAll locks every stripe (ascending) and returns the unlock function. Used
// by whole-store operations such as DropAll.
func (t *StripedMutex) LockAll() (unlock func()) {
	all := make([]int, stripedMutexSize)
	for i := range all {
		all[i] = i
	}
	return t.lockStripes(all)
}

func (t *StripedMutex) stripesFor(keys [][]byte) []int {
	seen := make(map[int]struct{}, len(keys))
	for _, k := range keys {
		seen[stripeIndex(k)] = struct{}{}
	}
	idx := make([]int, 0, len(seen))
	for i := range seen {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	return idx
}

func (t *StripedMutex) lockStripes(idx []int) (unlock func()) {
	for _, i := range idx {
		t.mus[i].Lock()
	}
	return func() {
		for j := len(idx) - 1; j >= 0; j-- {
			t.mus[idx[j]].Unlock()
		}
	}
}
