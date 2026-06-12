/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package store

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"google.golang.org/protobuf/proto"
)

type EnumerateOperation struct {
	DataStore                 // should be initialized
	Context    context.Context // should be initialized
	prefixBin []byte
	seekBin   []byte
	endBin    []byte
	batchSize int
	limit     int
	onlyKeys  bool
	reverse   bool
	paged     bool // Range/After/Limit require an ordered store
}

func (t *EnumerateOperation) ByPrefix(formatPrefix string, args... interface{}) *EnumerateOperation {
	if len(args) > 0 {
		t.prefixBin = []byte(fmt.Sprintf(formatPrefix, args...))
	} else {
		t.prefixBin = []byte(formatPrefix)
	}
	return t
}

func (t *EnumerateOperation) Seek(formatSeek string, args... interface{}) *EnumerateOperation {
	if len(args) > 0 {
		t.seekBin = []byte(fmt.Sprintf(formatSeek, args...))
	} else {
		t.seekBin = []byte(formatSeek)
	}
	return t
}

func (t *EnumerateOperation) ByRawPrefix(prefix []byte) *EnumerateOperation {
	t.prefixBin = prefix
	return t
}

// Range restricts enumeration to the half-open key interval [from, to): from is
// inclusive (the start position), to is exclusive (nil means unbounded). Requires
// an ordered store (OrderedCapability), otherwise Do/DoPage return ErrNotSupported.
func (t *EnumerateOperation) Range(from, to []byte) *EnumerateOperation {
	t.seekBin = from
	t.endBin = to
	t.paged = true
	return t
}

// After resumes enumeration strictly after the given continuation token (the last
// key from a previous page). A nil/empty token starts from the beginning.
func (t *EnumerateOperation) After(token []byte) *EnumerateOperation {
	if len(token) > 0 {
		t.seekBin = keySuccessor(token)
	}
	t.paged = true
	return t
}

// Limit caps the number of entries returned by Do/DoPage (a page size).
func (t *EnumerateOperation) Limit(n int) *EnumerateOperation {
	t.limit = n
	t.paged = true
	return t
}

func (t *EnumerateOperation) WithBatchSize(batchSize int) *EnumerateOperation {
	t.batchSize = batchSize
	return t
}

func (t *EnumerateOperation) OnlyKeys() *EnumerateOperation {
	t.onlyKeys = true
	return t
}

func (t *EnumerateOperation) Reverse() *EnumerateOperation {
	t.reverse = true
	return t
}

// enumerate runs the underlying scan applying the [start,end) upper bound and
// the limit, and returns the continuation token (the last emitted key) when the
// page was cut short by the limit, or nil when the range was exhausted.
func (t *EnumerateOperation) enumerate(cb func(*RawEntry) bool) (next []byte, err error) {
	if t.batchSize <= 0 {
		t.batchSize = DefaultBatchSize
	}
	if t.seekBin == nil {
		t.seekBin = t.prefixBin
	}
	if t.paged && !t.DataStore.Features().Has(OrderedCapability) {
		return nil, ErrNotSupported
	}

	count := 0
	hitLimit := false
	err = t.DataStore.EnumerateRaw(t.Context, t.prefixBin, t.seekBin, t.batchSize, t.onlyKeys, t.reverse, func(e *RawEntry) bool {
		if t.endBin != nil && bytes.Compare(e.Key, t.endBin) >= 0 {
			if t.reverse {
				return true // descending: skip keys at/above the exclusive upper bound
			}
			return false // ascending: reached the upper bound, stop
		}
		keep := cb(e)
		last := make([]byte, len(e.Key))
		copy(last, e.Key)
		next = last
		count++
		if !keep {
			return false
		}
		if t.limit > 0 && count >= t.limit {
			hitLimit = true
			return false
		}
		return true
	})
	if !hitLimit {
		next = nil
	}
	return next, err
}

// Do enumerates all matching entries (honoring Range/After/Limit if set).
func (t *EnumerateOperation) Do(cb func(*RawEntry) bool) error {
	_, err := t.enumerate(cb)
	return err
}

// DoPage is like Do but returns an opaque continuation token to pass to After
// for the next page. The token is nil when the range has been exhausted.
func (t *EnumerateOperation) DoPage(cb func(*RawEntry) bool) (next []byte, err error) {
	return t.enumerate(cb)
}

func (t *EnumerateOperation) DoProto(factory func() proto.Message, cb func(*ProtoEntry) bool) error {
	var marshalErr error
	_, err := t.enumerate(func(raw *RawEntry) bool {
		item := factory()
		if err := proto.Unmarshal(raw.Value, item); err != nil {
			marshalErr = err
			return false
		}
		pe := ProtoEntry{
			Key: raw.Key,
			Value: item,
			Ttl: raw.Ttl,
			Version: raw.Version,
		}
		return cb(&pe)
	})
	if err == nil {
		err = marshalErr
	}
	return err
}

func (t *EnumerateOperation) DoCounters(cb func(*CounterEntry) bool) (err error) {
	_, err = t.enumerate(func(raw *RawEntry) bool {
		var counter uint64
		if len(raw.Value) >= 8 {
			counter = binary.BigEndian.Uint64(raw.Value)
		}
		ce := CounterEntry{
			Key: raw.Key,
			Value: counter,
			Ttl: raw.Ttl,
			Version: raw.Version,
		}
		return cb(&ce)
	})
	return err
}

// keySuccessor returns the smallest key strictly greater than key (key + 0x00).
func keySuccessor(key []byte) []byte {
	s := make([]byte, len(key)+1)
	copy(s, key)
	return s
}
