/*
 * Copyright (c) 2023 Zander Schwid & Co. LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package store

import (
	"context"
	"fmt"
)

type IncrementOperation struct {
	DataStore                  // should be initialized
	Context        context.Context // should be initialized
	key        []byte
	ttlSeconds int
	Initial    int64
	Delta      int64   // should be initialized by 1
}

func (t *IncrementOperation) ByKey(formatKey string, args... interface{}) *IncrementOperation {
	if len(args) > 0 {
		t.key = []byte(fmt.Sprintf(formatKey, args...))
	} else {
		t.key = []byte(formatKey)
	}
	return t
}

func (t *IncrementOperation) ByRawKey(key []byte) *IncrementOperation {
	t.key = key
	return t
}

func (t *IncrementOperation) WithTtl(ttlSeconds int) *IncrementOperation {
	t.ttlSeconds = ttlSeconds
	return t
}

func (t *IncrementOperation) WithInitialValue(initial int64) *IncrementOperation {
	t.Initial = initial
	return t
}

func (t *IncrementOperation) WithDelta(delta int64) *IncrementOperation {
	t.Delta = delta
	return t
}

func (t *IncrementOperation) Do() (prev int64, err error) {
	return t.DataStore.IncrementRaw(t.Context, t.key, t.Initial, t.Delta, t.ttlSeconds)
}

