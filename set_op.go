/*
 * Copyright (c) 2023 Zander Schwid & Co. LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package store

import (
	"context"
	"encoding/binary"
	"fmt"
	"google.golang.org/protobuf/proto"
)

type SetOperation struct {
	DataStore                  // should be initialized
	Context        context.Context // should be initialized
	key        []byte
	ttlSeconds int
}

func (t *SetOperation) ByKey(formatKey string, args... interface{}) *SetOperation {
	if len(args) > 0 {
		t.key = []byte(fmt.Sprintf(formatKey, args...))
	} else {
		t.key = []byte(formatKey)
	}
	return t
}

func (t *SetOperation) ByRawKey(key []byte) *SetOperation {
	t.key = key
	return t
}

func (t *SetOperation) WithTtl(ttlSeconds int) *SetOperation {
	t.ttlSeconds = ttlSeconds
	return t
}

func (t *SetOperation) Binary(value []byte) error {
	return t.DataStore.SetRaw(t.Context, t.key, value, t.ttlSeconds)
}

func (t *SetOperation) String(value string) error {
	return t.DataStore.SetRaw(t.Context, t.key, []byte(value), t.ttlSeconds)
}

func (t *SetOperation) Counter(value uint64) error {
	slice := make([]byte, 8)
	binary.BigEndian.PutUint64(slice, value)
	return t.DataStore.SetRaw(t.Context, t.key, slice, t.ttlSeconds)
}

func (t *SetOperation) Proto(msg proto.Message) error {
	bin, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	return t.DataStore.SetRaw(t.Context, t.key, bin, t.ttlSeconds)
}
