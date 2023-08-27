/*
 * Copyright (c) 2023 Zander Schwid & Co. LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package store

import (
	"context"
	"fmt"
)

type RemoveOperation struct {
	DataStore                 // should be initialized
	Context       context.Context // should be initialized
	key       []byte
}

func (t *RemoveOperation) ByKey(formatKey string, args... interface{}) *RemoveOperation {
	if len(args) > 0 {
		t.key = []byte(fmt.Sprintf(formatKey, args...))
	} else {
		t.key = []byte(formatKey)
	}
	return t
}

func (t *RemoveOperation) ByRawKey(key []byte) *RemoveOperation {
	t.key = key
	return t
}

func (t *RemoveOperation) Do() error {
	return t.DataStore.RemoveRaw(t.Context, t.key)
}
