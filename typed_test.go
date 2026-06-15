/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package store

import (
	"context"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// mapDS is a minimal in-memory DataStore for exercising the typed helpers.
// Unused interface methods come from the embedded (nil) DataStore.
type mapDS struct {
	DataStore
	data map[string][]byte
}

func newMapDS() *mapDS { return &mapDS{data: map[string][]byte{}} }

func (m *mapDS) GetRaw(_ context.Context, key []byte, _ *int, _ *int64, required bool) ([]byte, error) {
	v, ok := m.data[string(key)]
	if !ok {
		if required {
			return nil, ErrNotFound
		}
		return nil, nil
	}
	out := make([]byte, len(v))
	copy(out, v)
	return out, nil
}

func (m *mapDS) SetRaw(_ context.Context, key, value []byte, _ int) error {
	m.data[string(key)] = append([]byte(nil), value...)
	return nil
}

func (m *mapDS) RemoveRaw(_ context.Context, key []byte) error {
	delete(m.data, string(key))
	return nil
}

func (m *mapDS) EnumerateRaw(_ context.Context, prefix, seek []byte, _ int, _ bool, _ bool, cb func(*RawEntry) bool) error {
	keys := make([]string, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !strings.HasPrefix(k, string(prefix)) || k < string(seek) {
			continue
		}
		if !cb(&RawEntry{Key: []byte(k), Value: m.data[k]}) {
			break
		}
	}
	return nil
}

type person struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func TestTypedJSON(t *testing.T) {
	ds := newMapDS()
	ctx := context.Background()
	codec := JSON[person]()

	if err := Put(ctx, ds, []byte("p:1"), person{Name: "Ann", Age: 30}, codec, NoTTL); err != nil {
		t.Fatal(err)
	}

	got, found, err := Get(ctx, ds, []byte("p:1"), codec)
	if err != nil || !found {
		t.Fatalf("get: found=%v err=%v", found, err)
	}
	if got.Name != "Ann" || got.Age != 30 {
		t.Fatalf("unexpected value: %+v", got)
	}

	// missing key -> found=false, zero value, no error
	_, found, err = Get(ctx, ds, []byte("p:missing"), codec)
	if err != nil || found {
		t.Fatalf("missing: found=%v err=%v", found, err)
	}
}

func TestTypedWrapperAndEnumerate(t *testing.T) {
	ds := newMapDS()
	ctx := context.Background()
	people := Of[person](ds, JSON[person]())

	for _, p := range []person{{"a", 1}, {"b", 2}, {"c", 3}} {
		if err := people.Put(ctx, []byte("p:"+p.Name), p, NoTTL); err != nil {
			t.Fatal(err)
		}
	}

	seen := map[string]int{}
	err := people.Enumerate(ctx, []byte("p:"), func(key []byte, value person) bool {
		seen[value.Name] = value.Age
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 3 || seen["a"] != 1 || seen["c"] != 3 {
		t.Fatalf("unexpected enumerate result: %v", seen)
	}

	if err := people.Remove(ctx, []byte("p:b")); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := people.Get(ctx, []byte("p:b")); found {
		t.Fatal("expected p:b removed")
	}
}

func TestTypedMsgPack(t *testing.T) {
	ds := newMapDS()
	ctx := context.Background()
	codec := MsgPack[person]()

	if err := Put(ctx, ds, []byte("p:1"), person{Name: "Bo", Age: 7}, codec, NoTTL); err != nil {
		t.Fatal(err)
	}
	got, found, err := Get(ctx, ds, []byte("p:1"), codec)
	if err != nil || !found {
		t.Fatalf("get: found=%v err=%v", found, err)
	}
	if got.Name != "Bo" || got.Age != 7 {
		t.Fatalf("unexpected value: %+v", got)
	}
}

func TestTypedProto(t *testing.T) {
	ds := newMapDS()
	ctx := context.Background()
	codec := Proto[*wrapperspb.StringValue]()

	if err := Put(ctx, ds, []byte("k:1"), wrapperspb.String("hello"), codec, NoTTL); err != nil {
		t.Fatal(err)
	}
	got, found, err := Get(ctx, ds, []byte("k:1"), codec)
	if err != nil || !found {
		t.Fatalf("get: found=%v err=%v", found, err)
	}
	if got.GetValue() != "hello" {
		t.Fatalf("unexpected proto value: %q", got.GetValue())
	}
}
