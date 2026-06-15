/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

// Package benchmarks runs the shared storetest benchmark suite against every
// engine in one place, so they can be compared head-to-head:
//
//	go test -bench . -benchmem ./benchmarks/...
//	go test -bench BenchmarkBadger/Set ./benchmarks/...
//
// It is a leaf module (nothing imports it) that pulls in all providers, which is
// why it lives apart from storetest. Under a plain `go test` (no -bench) it only
// compile-checks the harness against every provider.
package benchmarks_test

import (
	"os"
	"path/filepath"
	"testing"

	"go.arpabet.com/store"
	badgerstore "go.arpabet.com/store/providers/badger"
	bboltstore "go.arpabet.com/store/providers/bbolt"
	boltstore "go.arpabet.com/store/providers/bolt"
	memstore "go.arpabet.com/store/providers/mem"
	nutsdbstore "go.arpabet.com/store/providers/nutsdb"
	pebblestore "go.arpabet.com/store/providers/pebble"
	rosedbstore "go.arpabet.com/store/providers/rosedb"
	"go.arpabet.com/store/storetest"
)

func BenchmarkMem(b *testing.B) {
	storetest.RunBenchmarks(b, func(b *testing.B) store.ManagedDataStore {
		s := memstore.New("bench")
		b.Cleanup(func() { s.Destroy() })
		return s.Interface()
	})
}

func BenchmarkBadger(b *testing.B) {
	storetest.RunBenchmarks(b, func(b *testing.B) store.ManagedDataStore {
		s, err := badgerstore.New("bench", b.TempDir(), badgerstore.WithLogger(false))
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(func() { s.Destroy() })
		return s
	})
}

func BenchmarkPebble(b *testing.B) {
	storetest.RunBenchmarks(b, func(b *testing.B) store.ManagedDataStore {
		s, err := pebblestore.New("bench", b.TempDir(), nil)
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(func() { s.Destroy() })
		return s.Interface()
	})
}

func BenchmarkBbolt(b *testing.B) {
	storetest.RunBenchmarks(b, func(b *testing.B) store.ManagedDataStore {
		s, err := bboltstore.New("bench", filepath.Join(b.TempDir(), "bench.db"), os.FileMode(0600))
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(func() { s.Destroy() })
		return s.Interface()
	})
}

func BenchmarkBolt(b *testing.B) {
	storetest.RunBenchmarks(b, func(b *testing.B) store.ManagedDataStore {
		s, err := boltstore.New("bench", filepath.Join(b.TempDir(), "bench.db"), os.FileMode(0600))
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(func() { s.Destroy() })
		return s.Interface()
	})
}

func BenchmarkRosedb(b *testing.B) {
	storetest.RunBenchmarks(b, func(b *testing.B) store.ManagedDataStore {
		s, err := rosedbstore.New("bench", b.TempDir())
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(func() { s.Destroy() })
		return s
	})
}

func BenchmarkNutsdb(b *testing.B) {
	storetest.RunBenchmarks(b, func(b *testing.B) store.ManagedDataStore {
		s, err := nutsdbstore.New("bench", b.TempDir())
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(func() { s.Destroy() })
		return s
	})
}
