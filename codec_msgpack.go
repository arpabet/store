/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package store

import (
	msgpack "github.com/vmihailenco/msgpack/v5"
)

// MsgPack returns a Codec[T] using MessagePack (github.com/vmihailenco/msgpack).
// Good for compact, schema-less encoding of arbitrary Go values.
func MsgPack[T any]() Codec[T] {
	return msgpackCodec[T]{}
}

type msgpackCodec[T any] struct{}

func (msgpackCodec[T]) Encode(value T) ([]byte, error) {
	return msgpack.Marshal(value)
}

func (msgpackCodec[T]) Decode(data []byte) (T, error) {
	var v T
	err := msgpack.Unmarshal(data, &v)
	return v, err
}
