/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

// Package storetest provides a capability-gated conformance suite that every
// store provider (and middleware) must pass. It is the contract that makes
// providers interchangeable: a behavior is asserted for a provider only if the
// provider advertises the corresponding Capability via Features(), and a
// provider must never advertise a capability it does not honor.
//
// Providers wire it up from a _test.go file:
//
//	func TestConformance(t *testing.T) {
//	    storetest.RunConformance(t, func(t *testing.T) store.ManagedDataStore {
//	        s, err := myprovider.New(...)
//	        require.NoError(t, err)
//	        t.Cleanup(func() { s.Destroy() })
//	        return s.Interface()
//	    })
//	}
package storetest

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.arpabet.com/store"
)

// Factory returns a fresh, empty store bound to t's lifecycle (the factory is
// responsible for registering teardown via t.Cleanup).
type Factory func(t *testing.T) store.ManagedDataStore

// keys are namespaced under a single "conf" bucket so the suite works on
// bucket-partitioned backends (bolt, bbolt) as well as flat ones.
const prefix = "conf:"

func key(name string) string { return prefix + name }

// RunConformance runs the full suite, gating each behavior on Features().
func RunConformance(t *testing.T, newStore Factory) {

	probe := newStore(t)
	caps := probe.Features()
	_, sweepable := probe.(store.Sweepable)
	t.Logf("capabilities: %s (sweepable=%v)", caps, sweepable)

	t.Run("GetSetRemove", func(t *testing.T) { testGetSetRemove(t, newStore) })
	t.Run("NotFound", func(t *testing.T) { testNotFound(t, newStore) })
	t.Run("Increment", func(t *testing.T) { testIncrement(t, newStore) })
	t.Run("Enumerate", func(t *testing.T) { testEnumerate(t, newStore) })
	t.Run("Batch", func(t *testing.T) { testBatch(t, newStore) })

	if caps.Has(store.BatchAtomicCapability) {
		t.Run("BatchAtomic", func(t *testing.T) { testBatchAtomic(t, newStore) })
	}
	if caps.Has(store.AtomicCapability) {
		t.Run("CompareAndSet", func(t *testing.T) { testCompareAndSet(t, newStore) })
		t.Run("Version", func(t *testing.T) { testVersion(t, newStore) })
	}
	if caps.Has(store.TTLCapability) {
		t.Run("TTL", func(t *testing.T) { testTTL(t, newStore) })
		t.Run("Expiry", func(t *testing.T) { testExpiry(t, newStore) })
		t.Run("Touch", func(t *testing.T) { testTouch(t, newStore) })
		t.Run("BatchTTL", func(t *testing.T) { testBatchTTL(t, newStore) })
		if sweepable {
			t.Run("Sweep", func(t *testing.T) { testSweep(t, newStore) })
		}
	}
	if caps.Has(store.OrderedCapability) {
		t.Run("Ordered", func(t *testing.T) { testOrdered(t, newStore) })
		t.Run("Range", func(t *testing.T) { testRange(t, newStore) })
		t.Run("RangeReverse", func(t *testing.T) { testRangeReverse(t, newStore) })
		t.Run("Pagination", func(t *testing.T) { testPagination(t, newStore) })
	}
	if caps.Has(store.WatchCapability) {
		t.Run("Watch", func(t *testing.T) { testWatch(t, newStore) })
	}
}

func testGetSetRemove(t *testing.T, newStore Factory) {
	s := newStore(t)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx).ByKey("%s", key("a")).String("alpha"))

	val, err := s.Get(ctx).ByKey("%s", key("a")).ToString()
	require.NoError(t, err)
	require.Equal(t, "alpha", val)

	// overwrite
	require.NoError(t, s.Set(ctx).ByKey("%s", key("a")).String("alpha2"))
	val, err = s.Get(ctx).ByKey("%s", key("a")).ToString()
	require.NoError(t, err)
	require.Equal(t, "alpha2", val)

	require.NoError(t, s.Remove(ctx).ByKey("%s", key("a")).Do())
	val, err = s.Get(ctx).ByKey("%s", key("a")).ToString()
	require.NoError(t, err)
	require.Equal(t, "", val)
}

func testNotFound(t *testing.T, newStore Factory) {
	s := newStore(t)
	ctx := context.Background()

	// non-required missing key returns empty, no error
	val, err := s.Get(ctx).ByKey("%s", key("missing")).ToString()
	require.NoError(t, err)
	require.Equal(t, "", val)

	// required missing key returns ErrNotFound
	_, err = s.Get(ctx).ByKey("%s", key("missing")).Required().ToString()
	require.ErrorIs(t, err, store.ErrNotFound)
}

func testIncrement(t *testing.T, newStore Factory) {
	s := newStore(t)
	ctx := context.Background()

	prev, err := s.Increment(ctx).ByKey("%s", key("counter")).Do()
	require.NoError(t, err)
	require.Equal(t, int64(0), prev)

	prev, err = s.Increment(ctx).ByKey("%s", key("counter")).WithDelta(5).Do()
	require.NoError(t, err)
	require.Equal(t, int64(1), prev)

	cur, err := s.Get(ctx).ByKey("%s", key("counter")).ToCounter()
	require.NoError(t, err)
	require.Equal(t, uint64(6), cur)
}

func testEnumerate(t *testing.T, newStore Factory) {
	s := newStore(t)
	ctx := context.Background()

	want := map[string]string{key("a"): "1", key("b"): "2", key("c"): "3"}
	for k, v := range want {
		require.NoError(t, s.Set(ctx).ByRawKey([]byte(k)).String(v))
	}

	got := map[string]string{}
	err := s.Enumerate(ctx).ByPrefix(prefix).Do(func(e *store.RawEntry) bool {
		got[string(e.Key)] = string(e.Value)
		return true
	})
	require.NoError(t, err)
	require.Equal(t, want, got)

	// only-keys enumeration yields keys without values
	count := 0
	err = s.Enumerate(ctx).ByPrefix(prefix).OnlyKeys().Do(func(e *store.RawEntry) bool {
		require.Empty(t, e.Value)
		count++
		return true
	})
	require.NoError(t, err)
	require.Equal(t, 3, count)
}

func testBatch(t *testing.T, newStore Factory) {
	s := newStore(t)
	ctx := context.Background()

	// empty batch is a no-op
	require.NoError(t, s.Batch(ctx).Do())

	b := s.Batch(ctx)
	for i := 0; i < 5; i++ {
		b.Put([]byte(key(fmt.Sprintf("b%d", i))), []byte(fmt.Sprintf("v%d", i)), store.NoTTL)
	}
	require.NoError(t, b.Do())

	for i := 0; i < 5; i++ {
		v, err := s.Get(ctx).ByRawKey([]byte(key(fmt.Sprintf("b%d", i)))).ToString()
		require.NoError(t, err)
		require.Equal(t, fmt.Sprintf("v%d", i), v)
	}

	// duplicate key within one batch: last value wins
	require.NoError(t, s.Batch(ctx).
		Put([]byte(key("dup")), []byte("first"), store.NoTTL).
		Put([]byte(key("dup")), []byte("second"), store.NoTTL).
		Do())
	v, err := s.Get(ctx).ByRawKey([]byte(key("dup"))).ToString()
	require.NoError(t, err)
	require.Equal(t, "second", v)
}

// testBatchAtomic verifies all-or-nothing: a batch that fails (here via a
// cancelled context before commit) leaves none of its entries written.
func testBatchAtomic(t *testing.T, newStore Factory) {
	s := newStore(t)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.Batch(cancelled).
		Put([]byte(key("atom1")), []byte("x"), store.NoTTL).
		Put([]byte(key("atom2")), []byte("y"), store.NoTTL).
		Do()
	require.Error(t, err, "cancelled batch must fail")

	ctx := context.Background()
	for _, k := range []string{"atom1", "atom2"} {
		v, err := s.Get(ctx).ByRawKey([]byte(key(k))).ToString()
		require.NoError(t, err)
		require.Equal(t, "", v, "no entry from a failed atomic batch should be written")
	}
}

func testCompareAndSet(t *testing.T, newStore Factory) {
	s := newStore(t)
	ctx := context.Background()

	// create-only: version 0 succeeds when absent
	ok, err := s.CompareAndSet(ctx).ByKey("%s", key("cas")).WithVersion(0).String("v1")
	require.NoError(t, err)
	require.True(t, ok)

	// a second create-only must fail (key now exists)
	ok, err = s.CompareAndSet(ctx).ByKey("%s", key("cas")).WithVersion(0).String("dup")
	require.NoError(t, err)
	require.False(t, ok)

	var ver int64
	val, err := s.Get(ctx).ByKey("%s", key("cas")).WithVersion(&ver).ToString()
	require.NoError(t, err)
	require.Equal(t, "v1", val)

	// stale version fails
	ok, err = s.CompareAndSet(ctx).ByKey("%s", key("cas")).WithVersion(ver + 100000).String("nope")
	require.NoError(t, err)
	require.False(t, ok)

	// correct version succeeds
	ok, err = s.CompareAndSet(ctx).ByKey("%s", key("cas")).WithVersion(ver).String("v2")
	require.NoError(t, err)
	require.True(t, ok)

	// replaying the old version now fails (version moved on)
	ok, err = s.CompareAndSet(ctx).ByKey("%s", key("cas")).WithVersion(ver).String("v3")
	require.NoError(t, err)
	require.False(t, ok)

	val, err = s.Get(ctx).ByKey("%s", key("cas")).ToString()
	require.NoError(t, err)
	require.Equal(t, "v2", val)
}

func testVersion(t *testing.T, newStore Factory) {
	s := newStore(t)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx).ByKey("%s", key("v")).String("one"))
	var v1 int64
	_, err := s.Get(ctx).ByKey("%s", key("v")).WithVersion(&v1).ToString()
	require.NoError(t, err)

	require.NoError(t, s.Set(ctx).ByKey("%s", key("v")).String("two"))
	var v2 int64
	_, err = s.Get(ctx).ByKey("%s", key("v")).WithVersion(&v2).ToString()
	require.NoError(t, err)

	require.NotEqual(t, v1, v2, "version must change on update")
}

func testTTL(t *testing.T, newStore Factory) {
	s := newStore(t)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx).ByKey("%s", key("ttl")).WithTtl(100).String("x"))
	var ttl int
	_, err := s.Get(ctx).ByKey("%s", key("ttl")).WithTtl(&ttl).ToString()
	require.NoError(t, err)
	require.True(t, ttl > 0 && ttl <= 100, "expected remaining ttl in (0,100], got %d", ttl)

	// no-ttl write reports NoTTL
	require.NoError(t, s.Set(ctx).ByKey("%s", key("nottl")).String("x"))
	ttl = -999
	_, err = s.Get(ctx).ByKey("%s", key("nottl")).WithTtl(&ttl).ToString()
	require.NoError(t, err)
	require.Equal(t, store.NoTTL, ttl)
}

func testExpiry(t *testing.T, newStore Factory) {
	s := newStore(t)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx).ByKey("%s", key("exp")).WithTtl(1).String("gone"))
	time.Sleep(1500 * time.Millisecond)

	val, err := s.Get(ctx).ByKey("%s", key("exp")).ToString()
	require.NoError(t, err)
	require.Equal(t, "", val, "entry should have expired")
}

// testSweep verifies that a Sweepable store physically removes expired entries
// (not merely hides them), leaves live entries intact, is idempotent, and emits
// a WatchDelete during the sweep.
func testSweep(t *testing.T, newStore Factory) {
	s := newStore(t)
	sw := s.(store.Sweepable)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx).ByKey("%s", key("live")).String("keep"))
	require.NoError(t, s.Set(ctx).ByKey("%s", key("gone")).WithTtl(1).String("bye"))
	time.Sleep(1500 * time.Millisecond)

	// watch for the delete the sweep is about to emit
	events := make(chan *store.WatchEvent, 8)
	wctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = s.Watch(wctx).ByPrefix(prefix).Do(func(e *store.WatchEvent) bool {
			events <- e
			return true
		})
	}()
	time.Sleep(150 * time.Millisecond)

	removed, err := sw.SweepExpired(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, removed, 1, "sweep should remove the expired key")

	// live key survived, expired key gone
	live, err := s.Get(ctx).ByKey("%s", key("live")).ToString()
	require.NoError(t, err)
	require.Equal(t, "keep", live)
	gone, err := s.Get(ctx).ByKey("%s", key("gone")).ToString()
	require.NoError(t, err)
	require.Equal(t, "", gone)

	// idempotent: nothing left to sweep
	removed2, err := sw.SweepExpired(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, removed2)

	// the sweep emitted a WatchDelete for the expired key
	select {
	case e := <-events:
		require.Equal(t, store.WatchDelete, e.Type)
		require.Equal(t, key("gone"), string(e.Key))
	case <-time.After(3 * time.Second):
		t.Fatal("no WatchDelete emitted during sweep")
	}
}

func testTouch(t *testing.T, newStore Factory) {
	s := newStore(t)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx).ByKey("%s", key("touch")).String("x")) // no ttl
	require.NoError(t, s.Touch(ctx).ByKey("%s", key("touch")).WithTtl(100).Do())

	var ttl int
	_, err := s.Get(ctx).ByKey("%s", key("touch")).WithTtl(&ttl).ToString()
	require.NoError(t, err)
	require.True(t, ttl > 0 && ttl <= 100, "touch should set ttl, got %d", ttl)
}

func testRange(t *testing.T, newStore Factory) {
	s := newStore(t)
	ctx := context.Background()

	for _, name := range []string{"a", "b", "c", "d", "e"} {
		require.NoError(t, s.Set(ctx).ByRawKey([]byte(key(name))).String(name))
	}

	// [b, d) -> b, c (d exclusive)
	var got []string
	err := s.Enumerate(ctx).Range([]byte(key("b")), []byte(key("d"))).Do(func(e *store.RawEntry) bool {
		got = append(got, string(e.Key))
		return true
	})
	require.NoError(t, err)
	require.Equal(t, []string{key("b"), key("c")}, got)

	// open upper bound [d, nil) -> d, e
	got = nil
	err = s.Enumerate(ctx).Range([]byte(key("d")), nil).Do(func(e *store.RawEntry) bool {
		got = append(got, string(e.Key))
		return true
	})
	require.NoError(t, err)
	require.Equal(t, []string{key("d"), key("e")}, got)
}

// testRangeReverse verifies descending enumeration: a plain reverse scan returns
// keys in descending order, and a reverse Range over [from, to) returns the same
// keys as the forward scan but in reverse (from inclusive, to exclusive).
func testRangeReverse(t *testing.T, newStore Factory) {
	s := newStore(t)
	ctx := context.Background()

	for _, name := range []string{"a", "b", "c", "d", "e"} {
		require.NoError(t, s.Set(ctx).ByRawKey([]byte(key(name))).String(name))
	}

	// full reverse: e, d, c, b, a
	var got []string
	err := s.Enumerate(ctx).ByPrefix(prefix).Reverse().Do(func(e *store.RawEntry) bool {
		got = append(got, string(e.Key))
		return true
	})
	require.NoError(t, err)
	require.Equal(t, []string{key("e"), key("d"), key("c"), key("b"), key("a")}, got)

	// reverse [b, d) -> c, b (d exclusive, b inclusive)
	got = nil
	err = s.Enumerate(ctx).Range([]byte(key("b")), []byte(key("d"))).Reverse().Do(func(e *store.RawEntry) bool {
		got = append(got, string(e.Key))
		return true
	})
	require.NoError(t, err)
	require.Equal(t, []string{key("c"), key("b")}, got)
}

// testBatchTTL verifies that per-entry TTLs in a batch are honored: an entry with
// a short TTL expires while one with a long TTL survives and reports its TTL.
func testBatchTTL(t *testing.T, newStore Factory) {
	s := newStore(t)
	ctx := context.Background()

	require.NoError(t, s.Batch(ctx).
		Put([]byte(key("bt_short")), []byte("x"), 1).
		Put([]byte(key("bt_long")), []byte("y"), 100).
		Do())

	var ttl int
	_, err := s.Get(ctx).ByRawKey([]byte(key("bt_long"))).WithTtl(&ttl).ToString()
	require.NoError(t, err)
	require.True(t, ttl > 0 && ttl <= 100, "long batch entry should report remaining ttl, got %d", ttl)

	time.Sleep(1500 * time.Millisecond)

	v, err := s.Get(ctx).ByRawKey([]byte(key("bt_short"))).ToString()
	require.NoError(t, err)
	require.Equal(t, "", v, "short-ttl batch entry should have expired")

	v, err = s.Get(ctx).ByRawKey([]byte(key("bt_long"))).ToString()
	require.NoError(t, err)
	require.Equal(t, "y", v, "long-ttl batch entry should still be present")
}

func testPagination(t *testing.T, newStore Factory) {
	s := newStore(t)
	ctx := context.Background()

	const n = 5
	for i := 0; i < n; i++ {
		require.NoError(t, s.Set(ctx).ByRawKey([]byte(key(fmt.Sprintf("p%d", i)))).String(fmt.Sprintf("v%d", i)))
	}

	var all []string
	var token []byte
	pages := 0
	for {
		var page []string
		next, err := s.Enumerate(ctx).ByPrefix("%s", key("p")).After(token).Limit(2).DoPage(func(e *store.RawEntry) bool {
			page = append(page, string(e.Key))
			return true
		})
		require.NoError(t, err)
		all = append(all, page...)
		pages++
		require.LessOrEqual(t, len(page), 2, "page must not exceed the limit")
		if next == nil {
			break
		}
		token = next
		require.Less(t, pages, 10, "pagination did not terminate")
	}

	want := make([]string, n)
	for i := 0; i < n; i++ {
		want[i] = key(fmt.Sprintf("p%d", i))
	}
	require.Equal(t, want, all)
	require.GreaterOrEqual(t, pages, 3, "5 items at page size 2 should take at least 3 pages")
}

func testOrdered(t *testing.T, newStore Factory) {
	s := newStore(t)
	ctx := context.Background()

	for _, name := range []string{"c", "a", "b"} {
		require.NoError(t, s.Set(ctx).ByRawKey([]byte(key(name))).String(name))
	}

	var keys []string
	err := s.Enumerate(ctx).ByPrefix(prefix).Do(func(e *store.RawEntry) bool {
		keys = append(keys, string(e.Key))
		return true
	})
	require.NoError(t, err)
	require.True(t, sort.StringsAreSorted(keys), "keys must be returned in sorted order: %v", keys)
}

func testWatch(t *testing.T, newStore Factory) {
	s := newStore(t)

	events := make(chan *store.WatchEvent, 32)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- s.Watch(ctx).ByPrefix(prefix).Do(func(e *store.WatchEvent) bool {
			events <- e
			return true
		})
	}()

	// Produce repeatedly until an event is observed: this absorbs the
	// inherent race between the watcher registering and the first write.
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()

	var got *store.WatchEvent
wait:
	for {
		select {
		case got = <-events:
			break wait
		case <-deadline:
			cancel()
			t.Fatal("no watch event received within timeout")
		case <-tick.C:
			require.NoError(t, s.Set(context.Background()).ByKey("%s", key("w")).String("hello"))
		}
	}

	require.Equal(t, key("w"), string(got.Key))
	require.Equal(t, store.WatchSet, got.Type)

	// a remove must surface as WatchDelete (validates native delete decoding on
	// badger and the hub delete path elsewhere); earlier duplicate WatchSet
	// events from the produce loop may arrive first.
	require.NoError(t, s.Remove(context.Background()).ByKey("%s", key("w")).Do())
	delDeadline := time.After(5 * time.Second)
delwait:
	for {
		select {
		case e := <-events:
			if e.Type == store.WatchDelete {
				require.Equal(t, key("w"), string(e.Key))
				break delwait
			}
		case <-delDeadline:
			cancel()
			t.Fatal("no WatchDelete received after Remove")
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("watch did not return after context cancel")
	}
}

// RotatableFactory returns a fresh store together with a rotate function that
// switches the store to a new active encryption key (the rotation point). It is
// used by RunRotation and lives outside RunConformance because rotation is a
// property of an encrypted/key-managed store, not of every backend.
type RotatableFactory func(t *testing.T) (s store.ManagedDataStore, rotate func() error)

// RunRotation asserts online key rotation through the public API: values written
// before a rotation must still read back afterward (decrypted under their
// original key), new writes must succeed under the new active key, and an
// existing key overwritten after rotation must read back its new value. It makes
// no assumptions about the at-rest format, so any store that can rotate its
// active key can reuse it.
func RunRotation(t *testing.T, newStore RotatableFactory) {
	s, rotate := newStore(t)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx).ByKey("%s", key("old")).String("old-value"))

	require.NoError(t, rotate(), "rotation must succeed")

	require.NoError(t, s.Set(ctx).ByKey("%s", key("new")).String("new-value"))

	// a value written before rotation still decrypts under its original key
	old, err := s.Get(ctx).ByKey("%s", key("old")).ToString()
	require.NoError(t, err)
	require.Equal(t, "old-value", old, "pre-rotation value must remain readable")

	// a value written after rotation reads back under the new active key
	got, err := s.Get(ctx).ByKey("%s", key("new")).ToString()
	require.NoError(t, err)
	require.Equal(t, "new-value", got)

	// overwriting a pre-rotation key after rotation re-seals it under the new key
	require.NoError(t, s.Set(ctx).ByKey("%s", key("old")).String("rewritten"))
	got, err = s.Get(ctx).ByKey("%s", key("old")).ToString()
	require.NoError(t, err)
	require.Equal(t, "rewritten", got)
}
