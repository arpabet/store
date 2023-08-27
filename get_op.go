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

type GetOperation struct {
	DataStore                 // should be initialized
	Context       context.Context // should be initialized
	key       []byte
	required  bool
	ttlPtr *int
	versionPtr *int64
}

func (t *GetOperation) Required() *GetOperation {
	t.required = true
	return t
}

func (t *GetOperation) ByKey(formatKey string, args... interface{}) *GetOperation {
	if len(args) > 0 {
		t.key = []byte(fmt.Sprintf(formatKey, args...))
	} else {
		t.key = []byte(formatKey)
	}
	return t
}

func (t *GetOperation) ByRawKey(key []byte) *GetOperation {
	t.key = key
	return t
}

func (t *GetOperation) WithVersion(version *int64) *GetOperation {
	t.versionPtr = version
	return t
}

func (t *GetOperation) WithTtl(ttl *int) *GetOperation {
	t.ttlPtr = ttl
	return t
}

func (t *GetOperation) ToProto(container proto.Message) error {
	value, err := t.GetRaw(t.Context, t.key, t.ttlPtr, t.versionPtr, t.required)
	if err != nil || value == nil {
		return err
	}
	return proto.Unmarshal(value, container)
}

func (t *GetOperation) ToBinary() ([]byte, error) {
	return t.GetRaw(t.Context, t.key, t.ttlPtr, t.versionPtr, t.required)
}

func (t *GetOperation) ToString() (string, error) {
	content, err :=  t.GetRaw(t.Context, t.key, t.ttlPtr, t.versionPtr, t.required)
	if err != nil || content == nil {
		return "", err
	}
	return string(content), nil
}

func (t *GetOperation) ToCounter() (uint64, error) {
	content, err :=  t.GetRaw(t.Context, t.key, t.ttlPtr, t.versionPtr, t.required)
	if err != nil || len(content) < 8 {
		return 0, err
	}
	return binary.BigEndian.Uint64(content), nil
}

func (t *GetOperation) ToEntry() (entry RawEntry, err error) {
	entry.Key = t.key
	entry.Value, err = t.GetRaw(t.Context, t.key, &entry.Ttl, &entry.Version, t.required)
	return
}

func (t *GetOperation) ToProtoEntry(factory func() proto.Message) (entry ProtoEntry, err error) {
	entry.Key = t.key
	var value []byte
	if value, err = t.GetRaw(t.Context, t.key, &entry.Ttl, &entry.Version, t.required); err != nil {
		return
	}
	if value != nil {
		entry.Value = factory()
		err = proto.Unmarshal(value, entry.Value)
	}
	return
}

func (t *GetOperation) ToCounterEntry() (entry CounterEntry, err error) {
	entry.Key = t.key
	var content []byte
	if content, err = t.GetRaw(t.Context, t.key, &entry.Ttl, &entry.Version, t.required); err != nil || len(content) < 8 {
		return
	}
	entry.Value = binary.BigEndian.Uint64(content)
	return
}