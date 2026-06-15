/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

// Package compress is a middleware that transparently compresses values for any
// store.ManagedDataStore. It wraps a delegate store, compressing values before
// they reach the backend and decompressing them on read.
//
// Each stored value carries a one-byte codec marker:
//
//	[ 1 byte codec ][ payload ]
//
// codec is one of None / Snappy / Zstd. Writes use the configured active codec,
// but if the compressed form is not smaller than the input (tiny or already
// compressed/encrypted data) the value is stored uncompressed with the None
// marker, so compression never inflates a value. Reads dispatch on the marker,
// so the active codec can change over time without breaking existing data.
//
// Keys are never touched (they must stay comparable for ordering/prefix scans);
// only values are. TTL and version metadata are handled by the underlying store.
//
// Compose compression OUTSIDE encryption (compress-then-encrypt): wrap as
// compress(crypto(base)) so plaintext is compressed before it is encrypted —
// encrypted bytes do not compress.
package compress

import (
	"context"
	"io"

	"github.com/klauspost/compress/snappy"
	"github.com/klauspost/compress/zstd"
	"go.arpabet.com/store"
)

// Codec identifies how a value's payload is encoded.
type Codec byte

const (
	None   Codec = 0
	Snappy Codec = 1
	Zstd   Codec = 2
)

type compressStore struct {
	delegate  store.ManagedDataStore
	codec     Codec
	minSize   int
	zstdLevel zstd.EncoderLevel

	enc *zstd.Encoder // nil unless the active codec is Zstd
	dec *zstd.Decoder // always available for reading Zstd values
}

type Option func(*compressStore)

// WithZstd selects zstd for writes at the given level (use zstd.SpeedDefault etc.).
func WithZstd(level zstd.EncoderLevel) Option {
	return func(t *compressStore) {
		t.codec = Zstd
		t.zstdLevel = level
	}
}

// WithSnappy selects snappy for writes (faster, lower ratio than zstd).
func WithSnappy() Option {
	return func(t *compressStore) { t.codec = Snappy }
}

// WithMinSize skips compression for values smaller than n bytes (stored None).
func WithMinSize(n int) Option {
	return func(t *compressStore) { t.minSize = n }
}

// New wraps delegate with compression. The default codec is zstd at the default
// level; values below 64 bytes are stored uncompressed.
func New(delegate store.ManagedDataStore, opts ...Option) (store.ManagedDataStore, error) {
	t := &compressStore{delegate: delegate, codec: Zstd, minSize: 64}
	t.zstdLevel = zstd.SpeedDefault
	for _, opt := range opts {
		opt(t)
	}

	// a decoder is always needed to read previously written zstd values
	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	t.dec = dec

	if t.codec == Zstd {
		enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(t.zstdLevel))
		if err != nil {
			return nil, err
		}
		t.enc = enc
	}
	return t, nil
}

func (t *compressStore) compress(value []byte) []byte {
	if t.codec == None || len(value) < t.minSize {
		return prepend(None, value)
	}
	var out []byte
	switch t.codec {
	case Snappy:
		out = snappy.Encode(nil, value)
	case Zstd:
		out = t.enc.EncodeAll(value, nil)
	default:
		return prepend(None, value)
	}
	if len(out) >= len(value) { // compression did not help; store as-is
		return prepend(None, value)
	}
	return prepend(t.codec, out)
}

func (t *compressStore) decompress(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return raw, nil
	}
	codec, payload := Codec(raw[0]), raw[1:]
	switch codec {
	case None:
		return clone(payload), nil
	case Snappy:
		return snappy.Decode(nil, payload)
	case Zstd:
		return t.dec.DecodeAll(payload, nil)
	default:
		return nil, store.ErrInternal
	}
}

func prepend(codec Codec, payload []byte) []byte {
	out := make([]byte, 1+len(payload))
	out[0] = byte(codec)
	copy(out[1:], payload)
	return out
}

func clone(b []byte) []byte {
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

// --- glue / management (delegate) ---

func (t *compressStore) BeanName() string                 { return t.delegate.BeanName() }
func (t *compressStore) Destroy() error                   { return t.delegate.Destroy() }
func (t *compressStore) Features() store.Capability       { return t.delegate.Features() }
func (t *compressStore) Compact(r float64) error          { return t.delegate.Compact(r) }
func (t *compressStore) Restore(r io.Reader) error        { return t.delegate.Restore(r) }
func (t *compressStore) DropAll() error                   { return t.delegate.DropAll() }
func (t *compressStore) DropWithPrefix(p []byte) error    { return t.delegate.DropWithPrefix(p) }
func (t *compressStore) Instance() interface{}            { return t.delegate.Instance() }
func (t *compressStore) Backup(w io.Writer, since uint64) (uint64, error) {
	return t.delegate.Backup(w, since)
}

// --- sugar (bind to this wrapper) ---

func (t *compressStore) Get(ctx context.Context) *store.GetOperation {
	return &store.GetOperation{DataStore: t, Context: ctx}
}
func (t *compressStore) Set(ctx context.Context) *store.SetOperation {
	return &store.SetOperation{DataStore: t, Context: ctx}
}
func (t *compressStore) Batch(ctx context.Context) *store.BatchOperation {
	return &store.BatchOperation{DataStore: t, Context: ctx}
}
func (t *compressStore) CompareAndSet(ctx context.Context) *store.CompareAndSetOperation {
	return &store.CompareAndSetOperation{DataStore: t, Context: ctx}
}
func (t *compressStore) Increment(ctx context.Context) *store.IncrementOperation {
	return &store.IncrementOperation{DataStore: t, Context: ctx, Initial: 0, Delta: 1}
}
func (t *compressStore) Touch(ctx context.Context) *store.TouchOperation {
	return &store.TouchOperation{DataStore: t, Context: ctx}
}
func (t *compressStore) Remove(ctx context.Context) *store.RemoveOperation {
	return &store.RemoveOperation{DataStore: t, Context: ctx}
}
func (t *compressStore) Enumerate(ctx context.Context) *store.EnumerateOperation {
	return &store.EnumerateOperation{DataStore: t, Context: ctx}
}
func (t *compressStore) Watch(ctx context.Context) *store.WatchOperation {
	return &store.WatchOperation{DataStore: t, Context: ctx}
}

// --- raw operations (compress/decompress around the delegate) ---

func (t *compressStore) GetRaw(ctx context.Context, key []byte, ttlPtr *int, versionPtr *int64, required bool) ([]byte, error) {
	raw, err := t.delegate.GetRaw(ctx, key, ttlPtr, versionPtr, required)
	if err != nil || raw == nil {
		return nil, err
	}
	return t.decompress(raw)
}

func (t *compressStore) SetRaw(ctx context.Context, key, value []byte, ttlSeconds int) error {
	return t.delegate.SetRaw(ctx, key, t.compress(value), ttlSeconds)
}

func (t *compressStore) SetBatchRaw(ctx context.Context, entries []store.RawEntry) error {
	out := make([]store.RawEntry, len(entries))
	for i := range entries {
		out[i] = store.RawEntry{Key: entries[i].Key, Value: t.compress(entries[i].Value), Ttl: entries[i].Ttl}
	}
	return t.delegate.SetBatchRaw(ctx, out)
}

func (t *compressStore) CompareAndSetRaw(ctx context.Context, key, value []byte, ttlSeconds int, version int64) (bool, error) {
	return t.delegate.CompareAndSetRaw(ctx, key, t.compress(value), ttlSeconds, version)
}

// IncrementRaw cannot delegate (the backend would do arithmetic on compressed
// bytes), so it performs the read-modify-write here. With an atomic delegate it
// uses an optimistic CAS retry loop; otherwise a (non-atomic) get/set.
func (t *compressStore) IncrementRaw(ctx context.Context, key []byte, initial, delta int64, ttlSeconds int) (int64, error) {
	atomic := t.delegate.Features().Has(store.AtomicCapability)
	const maxRetries = 100
	for attempt := 0; ; attempt++ {
		var version int64
		cur, err := t.GetRaw(ctx, key, nil, &version, false)
		if err != nil {
			return 0, err
		}
		prev := initial
		if len(cur) >= 8 {
			prev = int64(beUint64(cur))
		}
		next := prev + delta
		buf := bePutUint64(uint64(next))

		if atomic {
			ok, err := t.CompareAndSetRaw(ctx, key, buf, ttlSeconds, version)
			if err != nil {
				return 0, err
			}
			if ok {
				return prev, nil
			}
			if attempt >= maxRetries {
				return 0, store.ErrConcurrentTxn
			}
			continue
		}

		if err := t.SetRaw(ctx, key, buf, ttlSeconds); err != nil {
			return 0, err
		}
		return prev, nil
	}
}

func (t *compressStore) TouchRaw(ctx context.Context, key []byte, ttlSeconds int) error {
	return t.delegate.TouchRaw(ctx, key, ttlSeconds)
}

func (t *compressStore) RemoveRaw(ctx context.Context, key []byte) error {
	return t.delegate.RemoveRaw(ctx, key)
}

func (t *compressStore) EnumerateRaw(ctx context.Context, prefix, seek []byte, batchSize int, onlyKeys bool, reverse bool, cb func(*store.RawEntry) bool) error {
	var cbErr error
	err := t.delegate.EnumerateRaw(ctx, prefix, seek, batchSize, onlyKeys, reverse, func(e *store.RawEntry) bool {
		if !onlyKeys && len(e.Value) > 0 {
			plain, derr := t.decompress(e.Value)
			if derr != nil {
				cbErr = derr
				return false
			}
			e.Value = plain
		}
		return cb(e)
	})
	if err == nil {
		err = cbErr
	}
	return err
}

func (t *compressStore) WatchRaw(ctx context.Context, prefix []byte, cb func(*store.WatchEvent) bool) error {
	var cbErr error
	err := t.delegate.WatchRaw(ctx, prefix, func(e *store.WatchEvent) bool {
		if e.Type == store.WatchSet && len(e.Value) > 0 {
			plain, derr := t.decompress(e.Value)
			if derr != nil {
				cbErr = derr
				return false
			}
			e.Value = plain
		}
		return cb(e)
	})
	if err == nil {
		err = cbErr
	}
	return err
}

func beUint64(b []byte) uint64 {
	return uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
		uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7])
}

func bePutUint64(v uint64) []byte {
	return []byte{
		byte(v >> 56), byte(v >> 48), byte(v >> 40), byte(v >> 32),
		byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v),
	}
}
