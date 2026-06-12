/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

// Package otelstore is a middleware that wraps any store.ManagedDataStore with
// OpenTelemetry tracing. Every operation becomes a span carrying the bean name,
// the operation name and the key; values are never recorded, so it is safe to
// layer over (or under) cryptostore for PII workloads.
package otelstore

import (
	"context"
	"io"

	"go.arpabet.com/store"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const defaultTracerName = "go.arpabet.com/store"

type Option func(*otelStore)

// WithTracerProvider sets the TracerProvider used to create the tracer.
func WithTracerProvider(tp trace.TracerProvider) Option {
	return func(t *otelStore) { t.tracer = tp.Tracer(defaultTracerName) }
}

// WithTracerName overrides the instrumentation name of the tracer.
func WithTracerName(name string) Option {
	return func(t *otelStore) { t.tracer = otel.Tracer(name) }
}

type otelStore struct {
	delegate store.ManagedDataStore
	tracer   trace.Tracer
	beanAttr attribute.KeyValue
}

// New wraps delegate with tracing.
func New(delegate store.ManagedDataStore, opts ...Option) store.ManagedDataStore {
	t := &otelStore{
		delegate: delegate,
		tracer:   otel.Tracer(defaultTracerName),
		beanAttr: attribute.String("store.bean", delegate.BeanName()),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func (t *otelStore) start(ctx context.Context, op string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return t.tracer.Start(ctx, op,
		trace.WithAttributes(append([]attribute.KeyValue{t.beanAttr}, attrs...)...))
}

func finish(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

// --- glue / management ---

func (t *otelStore) BeanName() string            { return t.delegate.BeanName() }
func (t *otelStore) Destroy() error              { return t.delegate.Destroy() }
func (t *otelStore) Features() store.Capability  { return t.delegate.Features() }
func (t *otelStore) Instance() interface{}       { return t.delegate.Instance() }

func (t *otelStore) Compact(discardRatio float64) error {
	_, span := t.start(context.Background(), "store.Compact")
	err := t.delegate.Compact(discardRatio)
	finish(span, err)
	return err
}

func (t *otelStore) Backup(w io.Writer, since uint64) (uint64, error) {
	_, span := t.start(context.Background(), "store.Backup")
	v, err := t.delegate.Backup(w, since)
	finish(span, err)
	return v, err
}

func (t *otelStore) Restore(r io.Reader) error {
	_, span := t.start(context.Background(), "store.Restore")
	err := t.delegate.Restore(r)
	finish(span, err)
	return err
}

func (t *otelStore) DropAll() error {
	_, span := t.start(context.Background(), "store.DropAll")
	err := t.delegate.DropAll()
	finish(span, err)
	return err
}

func (t *otelStore) DropWithPrefix(prefix []byte) error {
	_, span := t.start(context.Background(), "store.DropWithPrefix", attribute.String("store.prefix", string(prefix)))
	err := t.delegate.DropWithPrefix(prefix)
	finish(span, err)
	return err
}

// --- sugar (bind to this wrapper) ---

func (t *otelStore) Get(ctx context.Context) *store.GetOperation {
	return &store.GetOperation{DataStore: t, Context: ctx}
}
func (t *otelStore) Set(ctx context.Context) *store.SetOperation {
	return &store.SetOperation{DataStore: t, Context: ctx}
}
func (t *otelStore) Batch(ctx context.Context) *store.BatchOperation {
	return &store.BatchOperation{DataStore: t, Context: ctx}
}
func (t *otelStore) CompareAndSet(ctx context.Context) *store.CompareAndSetOperation {
	return &store.CompareAndSetOperation{DataStore: t, Context: ctx}
}
func (t *otelStore) Increment(ctx context.Context) *store.IncrementOperation {
	return &store.IncrementOperation{DataStore: t, Context: ctx, Initial: 0, Delta: 1}
}
func (t *otelStore) Touch(ctx context.Context) *store.TouchOperation {
	return &store.TouchOperation{DataStore: t, Context: ctx}
}
func (t *otelStore) Remove(ctx context.Context) *store.RemoveOperation {
	return &store.RemoveOperation{DataStore: t, Context: ctx}
}
func (t *otelStore) Enumerate(ctx context.Context) *store.EnumerateOperation {
	return &store.EnumerateOperation{DataStore: t, Context: ctx}
}
func (t *otelStore) Watch(ctx context.Context) *store.WatchOperation {
	return &store.WatchOperation{DataStore: t, Context: ctx}
}

// --- raw operations (traced) ---

func (t *otelStore) GetRaw(ctx context.Context, key []byte, ttlPtr *int, versionPtr *int64, required bool) ([]byte, error) {
	ctx, span := t.start(ctx, "store.GetRaw", attribute.String("store.key", string(key)))
	val, err := t.delegate.GetRaw(ctx, key, ttlPtr, versionPtr, required)
	finish(span, err)
	return val, err
}

func (t *otelStore) SetRaw(ctx context.Context, key, value []byte, ttlSeconds int) error {
	ctx, span := t.start(ctx, "store.SetRaw",
		attribute.String("store.key", string(key)),
		attribute.Int("store.value_size", len(value)),
		attribute.Int("store.ttl", ttlSeconds))
	err := t.delegate.SetRaw(ctx, key, value, ttlSeconds)
	finish(span, err)
	return err
}

func (t *otelStore) SetBatchRaw(ctx context.Context, entries []store.RawEntry) error {
	ctx, span := t.start(ctx, "store.SetBatchRaw", attribute.Int("store.batch_size", len(entries)))
	err := t.delegate.SetBatchRaw(ctx, entries)
	finish(span, err)
	return err
}

func (t *otelStore) CompareAndSetRaw(ctx context.Context, key, value []byte, ttlSeconds int, version int64) (bool, error) {
	ctx, span := t.start(ctx, "store.CompareAndSetRaw",
		attribute.String("store.key", string(key)),
		attribute.Int64("store.version", version))
	ok, err := t.delegate.CompareAndSetRaw(ctx, key, value, ttlSeconds, version)
	span.SetAttributes(attribute.Bool("store.updated", ok))
	finish(span, err)
	return ok, err
}

func (t *otelStore) IncrementRaw(ctx context.Context, key []byte, initial, delta int64, ttlSeconds int) (int64, error) {
	ctx, span := t.start(ctx, "store.IncrementRaw",
		attribute.String("store.key", string(key)),
		attribute.Int64("store.delta", delta))
	prev, err := t.delegate.IncrementRaw(ctx, key, initial, delta, ttlSeconds)
	finish(span, err)
	return prev, err
}

func (t *otelStore) TouchRaw(ctx context.Context, key []byte, ttlSeconds int) error {
	ctx, span := t.start(ctx, "store.TouchRaw",
		attribute.String("store.key", string(key)),
		attribute.Int("store.ttl", ttlSeconds))
	err := t.delegate.TouchRaw(ctx, key, ttlSeconds)
	finish(span, err)
	return err
}

func (t *otelStore) RemoveRaw(ctx context.Context, key []byte) error {
	ctx, span := t.start(ctx, "store.RemoveRaw", attribute.String("store.key", string(key)))
	err := t.delegate.RemoveRaw(ctx, key)
	finish(span, err)
	return err
}

func (t *otelStore) EnumerateRaw(ctx context.Context, prefix, seek []byte, batchSize int, onlyKeys bool, reverse bool, cb func(*store.RawEntry) bool) error {
	ctx, span := t.start(ctx, "store.EnumerateRaw",
		attribute.String("store.prefix", string(prefix)),
		attribute.Bool("store.only_keys", onlyKeys),
		attribute.Bool("store.reverse", reverse))
	err := t.delegate.EnumerateRaw(ctx, prefix, seek, batchSize, onlyKeys, reverse, cb)
	finish(span, err)
	return err
}

func (t *otelStore) WatchRaw(ctx context.Context, prefix []byte, cb func(*store.WatchEvent) bool) error {
	ctx, span := t.start(ctx, "store.WatchRaw", attribute.String("store.prefix", string(prefix)))
	err := t.delegate.WatchRaw(ctx, prefix, cb)
	finish(span, err)
	return err
}
