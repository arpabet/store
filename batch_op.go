/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package store

import (
	"context"
	"encoding/binary"
	"fmt"

	"google.golang.org/protobuf/proto"
)

// BatchOperation accumulates entries and writes them in one SetBatchRaw call.
// The batch is atomic only on stores reporting BatchAtomicCapability.
type BatchOperation struct {
	DataStore                 // should be initialized
	Context   context.Context // should be initialized
	entries   []RawEntry
}

// Add appends a pre-built entry (Key, Value, Ttl are used).
func (t *BatchOperation) Add(entry RawEntry) *BatchOperation {
	t.entries = append(t.entries, entry)
	return t
}

// Put appends a binary key/value with optional ttl (NoTTL for none).
func (t *BatchOperation) Put(key, value []byte, ttlSeconds int) *BatchOperation {
	return t.Add(RawEntry{Key: key, Value: value, Ttl: ttlSeconds})
}

// PutString appends a string value under a formatted key.
func (t *BatchOperation) PutString(value string, ttlSeconds int, formatKey string, args ...interface{}) *BatchOperation {
	return t.Add(RawEntry{Key: formatKey2bytes(formatKey, args...), Value: []byte(value), Ttl: ttlSeconds})
}

// PutCounter appends a uint64 counter value under a formatted key.
func (t *BatchOperation) PutCounter(value uint64, ttlSeconds int, formatKey string, args ...interface{}) *BatchOperation {
	slice := make([]byte, 8)
	binary.BigEndian.PutUint64(slice, value)
	return t.Add(RawEntry{Key: formatKey2bytes(formatKey, args...), Value: slice, Ttl: ttlSeconds})
}

// PutProto appends a marshaled protobuf message under a formatted key.
func (t *BatchOperation) PutProto(msg proto.Message, ttlSeconds int, formatKey string, args ...interface{}) error {
	bin, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	t.Add(RawEntry{Key: formatKey2bytes(formatKey, args...), Value: bin, Ttl: ttlSeconds})
	return nil
}

// Len returns the number of accumulated entries.
func (t *BatchOperation) Len() int {
	return len(t.entries)
}

// Do writes all accumulated entries via SetBatchRaw.
func (t *BatchOperation) Do() error {
	if len(t.entries) == 0 {
		return nil
	}
	return t.DataStore.SetBatchRaw(t.Context, t.entries)
}

func formatKey2bytes(formatKey string, args ...interface{}) []byte {
	if len(args) > 0 {
		return []byte(fmt.Sprintf(formatKey, args...))
	}
	return []byte(formatKey)
}
