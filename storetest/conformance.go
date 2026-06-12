/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
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

	caps := newStore(t).Features()
	t.Logf("capabilities: %s", caps)

	t.Run("GetSetRemove", func(t *testing.T) { testGetSetRemove(t, newStore) })
	t.Run("NotFound", func(t *testing.T) { testNotFound(t, newStore) })
	t.Run("Increment", func(t *testing.T) { testIncrement(t, newStore) })
	t.Run("Enumerate", func(t *testing.T) { testEnumerate(t, newStore) })

	if caps.Has(store.AtomicCapability) {
		t.Run("CompareAndSet", func(t *testing.T) { testCompareAndSet(t, newStore) })
		t.Run("Version", func(t *testing.T) { testVersion(t, newStore) })
	}
	if caps.Has(store.TTLCapability) {
		t.Run("TTL", func(t *testing.T) { testTTL(t, newStore) })
		t.Run("Expiry", func(t *testing.T) { testExpiry(t, newStore) })
		t.Run("Touch", func(t *testing.T) { testTouch(t, newStore) })
	}
	if caps.Has(store.OrderedCapability) {
		t.Run("Ordered", func(t *testing.T) { testOrdered(t, newStore) })
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

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("watch did not return after context cancel")
	}
}
