/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package storetest

import (
	"context"
	"fmt"
	"testing"

	"go.arpabet.com/store"
)

// BenchFactory returns a fresh, empty store bound to b's lifecycle (the factory
// registers teardown via b.Cleanup). It mirrors Factory but for benchmarks.
type BenchFactory func(b *testing.B) store.ManagedDataStore

// benchValue is the fixed payload every write benchmark stores (128 bytes), so
// results compare engines on the same value size rather than on payload shape.
var benchValue = make([]byte, 128)

// benchSeedN is how many entries the read-side benchmarks (Get, Enumerate,
// Range, Sweep) pre-populate. Large enough to be representative, small enough
// that seeding sync-per-write engines does not dominate the run.
const benchSeedN = 5000

// benchKey formats the i-th key under the shared bench prefix so it works on
// bucket-partitioned backends (bolt, bbolt, nutsdb) as well as flat ones.
func benchKey(i int) []byte { return []byte(fmt.Sprintf("bench:%08d", i)) }

// seed writes n entries via batched writes so setup stays cheap even on
// sync-per-write engines (where n individual SetRaw calls would dominate the
// benchmark's wall time). Seeding is never part of a measured timer.
func seed(b *testing.B, s store.ManagedDataStore, n int) {
	b.Helper()
	ctx := context.Background()
	const chunk = 1000
	entries := make([]store.RawEntry, 0, chunk)
	for i := 0; i < n; i++ {
		entries = append(entries, store.RawEntry{Key: benchKey(i), Value: benchValue, Ttl: store.NoTTL})
		if len(entries) == chunk || i == n-1 {
			if err := s.SetBatchRaw(ctx, entries); err != nil {
				b.Fatal(err)
			}
			entries = entries[:0]
		}
	}
}

// RunBenchmarks runs the shared benchmark suite, gating each case on Features()
// exactly like RunConformance: a provider is measured on a capability only when
// it advertises it. Wire it from a provider's _test.go:
//
//	func BenchmarkMyProvider(b *testing.B) {
//	    storetest.RunBenchmarks(b, func(b *testing.B) store.ManagedDataStore {
//	        s, err := myprovider.New(...)
//	        if err != nil { b.Fatal(err) }
//	        b.Cleanup(func() { s.Destroy() })
//	        return s
//	    })
//	}
func RunBenchmarks(b *testing.B, newStore BenchFactory) {
	probe := newStore(b)
	caps := probe.Features()
	_, sweepable := probe.(store.Sweepable)

	b.Run("Set", func(b *testing.B) { benchSet(b, newStore) })
	b.Run("Get", func(b *testing.B) { benchGet(b, newStore) })
	b.Run("Batch", func(b *testing.B) { benchBatch(b, newStore) })

	if caps.Has(store.AtomicCapability) {
		b.Run("Increment", func(b *testing.B) { benchIncrement(b, newStore) })
		b.Run("CompareAndSet", func(b *testing.B) { benchCompareAndSet(b, newStore) })
	}
	if caps.Has(store.OrderedCapability) {
		b.Run("Enumerate", func(b *testing.B) { benchEnumerate(b, newStore) })
		b.Run("Range", func(b *testing.B) { benchRange(b, newStore) })
	}
	if caps.Has(store.TTLCapability) && sweepable {
		b.Run("Sweep", func(b *testing.B) { benchSweep(b, newStore) })
	}
}

func benchSet(b *testing.B, newStore BenchFactory) {
	s := newStore(b)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.SetRaw(ctx, benchKey(i), benchValue, store.NoTTL); err != nil {
			b.Fatal(err)
		}
	}
}

func benchGet(b *testing.B, newStore BenchFactory) {
	s := newStore(b)
	ctx := context.Background()
	const n = benchSeedN
	seed(b, s, n)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.GetRaw(ctx, benchKey(i%n), nil, nil, false); err != nil {
			b.Fatal(err)
		}
	}
}

func benchBatch(b *testing.B, newStore BenchFactory) {
	s := newStore(b)
	ctx := context.Background()
	const batch = 100
	entries := make([]store.RawEntry, batch)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		base := i * batch
		for j := 0; j < batch; j++ {
			entries[j] = store.RawEntry{Key: benchKey(base + j), Value: benchValue, Ttl: store.NoTTL}
		}
		if err := s.SetBatchRaw(ctx, entries); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(batch * len(benchValue)))
}

func benchIncrement(b *testing.B, newStore BenchFactory) {
	s := newStore(b)
	ctx := context.Background()
	key := []byte("bench:counter")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.IncrementRaw(ctx, key, 0, 1, store.NoTTL); err != nil {
			b.Fatal(err)
		}
	}
}

func benchCompareAndSet(b *testing.B, newStore BenchFactory) {
	s := newStore(b)
	ctx := context.Background()
	key := []byte("bench:cas")
	var version int64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ok, err := s.CompareAndSetRaw(ctx, key, benchValue, store.NoTTL, version)
		if err != nil {
			b.Fatal(err)
		}
		if !ok {
			b.Fatalf("CAS failed at version %d", version)
		}
		version++
	}
}

func benchEnumerate(b *testing.B, newStore BenchFactory) {
	s := newStore(b)
	ctx := context.Background()
	const n = benchSeedN
	seed(b, s, n)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		count := 0
		err := s.Enumerate(ctx).ByPrefix("bench:").Do(func(e *store.RawEntry) bool {
			count++
			return true
		})
		if err != nil {
			b.Fatal(err)
		}
		if count != n {
			b.Fatalf("expected %d entries, got %d", n, count)
		}
	}
}

func benchRange(b *testing.B, newStore BenchFactory) {
	s := newStore(b)
	ctx := context.Background()
	const n = benchSeedN
	for i := 0; i < n; i++ {
		if err := s.SetRaw(ctx, benchKey(i), benchValue, store.NoTTL); err != nil {
			b.Fatal(err)
		}
	}
	from, to := benchKey(n/4), benchKey(n/4*3) // middle half
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := s.Enumerate(ctx).Range(from, to).Do(func(e *store.RawEntry) bool { return true })
		if err != nil {
			b.Fatal(err)
		}
	}
}

// benchSweep measures the steady-state cost of a SweepExpired pass: a sweeper
// must scan every live entry to find the expired ones, so this seeds n live
// (non-expiring) entries once, outside the timer, then times repeated sweeps
// over them. That isolates the scan cost — the dominant term when little has
// expired — without per-iteration sleeps to force expiry.
func benchSweep(b *testing.B, newStore BenchFactory) {
	s := newStore(b)
	sw := s.(store.Sweepable)
	ctx := context.Background()
	const n = benchSeedN
	seed(b, s, n)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := sw.SweepExpired(ctx); err != nil {
			b.Fatal(err)
		}
	}
}
