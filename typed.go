/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package store

import (
	"context"
	"encoding/json"
	"reflect"

	"google.golang.org/protobuf/proto"
)

/**
Generic, codec-driven typed access on top of the raw byte API. This is pure sugar
and adds no requirements on providers.

	users := store.Of[*pb.User](ds, store.Proto[*pb.User]())
	users.Put(ctx, []byte("u:1"), &pb.User{Name: "Ann"}, store.NoTTL)
	u, found, err := users.Get(ctx, []byte("u:1"))

or as free functions:

	u, found, err := store.Get(ctx, ds, []byte("u:1"), store.Proto[*pb.User]())
*/

// Codec encodes/decodes values of type T to and from the stored byte form.
type Codec[T any] interface {
	Encode(value T) ([]byte, error)
	Decode(data []byte) (T, error)
}

// Get loads and decodes the value at key. found is false (with the zero T) when
// the key is absent.
func Get[T any](ctx context.Context, ds DataStore, key []byte, codec Codec[T]) (value T, found bool, err error) {
	data, err := ds.GetRaw(ctx, key, nil, nil, false)
	if err != nil || data == nil {
		return value, false, err
	}
	value, err = codec.Decode(data)
	if err != nil {
		var zero T
		return zero, false, err
	}
	return value, true, nil
}

// Put encodes and stores value at key with the given ttl (NoTTL for none).
func Put[T any](ctx context.Context, ds DataStore, key []byte, value T, codec Codec[T], ttlSeconds int) error {
	data, err := codec.Encode(value)
	if err != nil {
		return err
	}
	return ds.SetRaw(ctx, key, data, ttlSeconds)
}

// Typed binds a DataStore and a Codec[T] for ergonomic repeated typed access.
type Typed[T any] struct {
	ds    DataStore
	codec Codec[T]
}

// Of creates a typed view over ds using codec.
func Of[T any](ds DataStore, codec Codec[T]) Typed[T] {
	return Typed[T]{ds: ds, codec: codec}
}

func (t Typed[T]) Get(ctx context.Context, key []byte) (T, bool, error) {
	return Get[T](ctx, t.ds, key, t.codec)
}

func (t Typed[T]) Put(ctx context.Context, key []byte, value T, ttlSeconds int) error {
	return Put[T](ctx, t.ds, key, value, t.codec, ttlSeconds)
}

func (t Typed[T]) Remove(ctx context.Context, key []byte) error {
	return t.ds.RemoveRaw(ctx, key)
}

// Enumerate decodes every value under prefix, invoking cb until it returns false.
func (t Typed[T]) Enumerate(ctx context.Context, prefix []byte, cb func(key []byte, value T) bool) error {
	var decodeErr error
	err := t.ds.EnumerateRaw(ctx, prefix, prefix, DefaultBatchSize, false, false, func(e *RawEntry) bool {
		v, err := t.codec.Decode(e.Value)
		if err != nil {
			decodeErr = err
			return false
		}
		return cb(e.Key, v)
	})
	if err == nil {
		err = decodeErr
	}
	return err
}

// --- codecs ---

// JSON returns a Codec[T] using encoding/json.
func JSON[T any]() Codec[T] {
	return jsonCodec[T]{}
}

type jsonCodec[T any] struct{}

func (jsonCodec[T]) Encode(value T) ([]byte, error) {
	return json.Marshal(value)
}

func (jsonCodec[T]) Decode(data []byte) (T, error) {
	var v T
	err := json.Unmarshal(data, &v)
	return v, err
}

// Proto returns a Codec[T] using protobuf. T must be a concrete pointer message
// type, e.g. store.Proto[*pb.User]().
func Proto[T proto.Message]() Codec[T] {
	return protoCodec[T]{}
}

type protoCodec[T proto.Message] struct{}

func (protoCodec[T]) Encode(value T) ([]byte, error) {
	return proto.Marshal(value)
}

func (protoCodec[T]) Decode(data []byte) (T, error) {
	var zero T
	// allocate a fresh message of T's element type (T is a pointer message type)
	msg := reflect.New(reflect.TypeOf(zero).Elem()).Interface().(T)
	return msg, proto.Unmarshal(data, msg)
}
