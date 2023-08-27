/*
 * Copyright (c) 2023 Zander Schwid & Co. LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package store

import (
	"context"
	"fmt"
)

type TouchOperation struct {
	DataStore                 // should be initialized
	Context       context.Context // should be initialized
	key           []byte
	ttlSeconds    int
}

func (t *TouchOperation) ByKey(formatKey string, args... interface{}) *TouchOperation {
	if len(args) > 0 {
		t.key = []byte(fmt.Sprintf(formatKey, args...))
	} else {
		t.key = []byte(formatKey)
	}
	return t
}

func (t *TouchOperation) ByRawKey(key []byte) *TouchOperation {
	t.key = key
	return t
}

func (t *TouchOperation) WithTtl(ttlSeconds int) *TouchOperation {
	t.ttlSeconds = ttlSeconds
	return t
}

func (t *TouchOperation) Do() error {
	return t.DataStore.TouchRaw(t.Context, t.key, t.ttlSeconds)
}
