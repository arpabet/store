/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

// Package otelstore is a middleware that wraps any store.ManagedDataStore with
// OpenTelemetry observability: a span per operation plus metrics (operation
// count, error count, and a latency histogram). Spans and metrics carry the bean
// name and operation name; values are never recorded, so it is safe to layer
// over (or under) cryptostore for PII workloads. Prometheus users can scrape via
// the OpenTelemetry Prometheus bridge.
package otelstore

import (
	"context"
	"io"
	"time"

	"go.arpabet.com/store"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultTracerName = "go.arpabet.com/store"
	defaultMeterName  = "go.arpabet.com/store"
)

type Option func(*otelStore)

// WithTracerProvider sets the TracerProvider used to create the tracer.
func WithTracerProvider(tp trace.TracerProvider) Option {
	return func(t *otelStore) { t.tracer = tp.Tracer(defaultTracerName) }
}

// WithTracerName overrides the instrumentation name of the tracer.
func WithTracerName(name string) Option {
	return func(t *otelStore) { t.tracer = otel.Tracer(name) }
}

// WithMeterProvider sets the MeterProvider used to create metric instruments.
func WithMeterProvider(mp metric.MeterProvider) Option {
	return func(t *otelStore) { t.meter = mp.Meter(defaultMeterName) }
}

// WithMeterName overrides the instrumentation name of the meter.
func WithMeterName(name string) Option {
	return func(t *otelStore) { t.meter = otel.Meter(name) }
}

type otelStore struct {
	delegate store.ManagedDataStore
	tracer   trace.Tracer
	meter    metric.Meter
	beanAttr attribute.KeyValue

	ops     metric.Int64Counter
	errs    metric.Int64Counter
	latency metric.Float64Histogram
}

// New wraps delegate with tracing and metrics. With no providers configured it
// uses the global OpenTelemetry providers (no-ops until the app installs SDKs).
func New(delegate store.ManagedDataStore, opts ...Option) store.ManagedDataStore {
	t := &otelStore{
		delegate: delegate,
		tracer:   otel.Tracer(defaultTracerName),
		meter:    otel.Meter(defaultMeterName),
		beanAttr: attribute.String("store.bean", delegate.BeanName()),
	}
	for _, opt := range opts {
		opt(t)
	}
	t.ops, _ = t.meter.Int64Counter("store.operations",
		metric.WithDescription("number of store operations"))
	t.errs, _ = t.meter.Int64Counter("store.errors",
		metric.WithDescription("number of failed store operations"))
	t.latency, _ = t.meter.Float64Histogram("store.operation.duration",
		metric.WithUnit("s"), metric.WithDescription("store operation latency in seconds"))
	return t
}

// begin starts a span and a timer; the returned done(err) records metrics and
// ends the span.
func (t *otelStore) begin(ctx context.Context, op string, attrs ...attribute.KeyValue) (context.Context, trace.Span, func(error)) {
	ctx, span := t.tracer.Start(ctx, op,
		trace.WithAttributes(append([]attribute.KeyValue{t.beanAttr}, attrs...)...))
	start := time.Now()
	done := func(err error) {
		set := metric.WithAttributes(t.beanAttr, attribute.String("store.operation", op))
		t.ops.Add(ctx, 1, set)
		t.latency.Record(ctx, time.Since(start).Seconds(), set)
		if err != nil {
			t.errs.Add(ctx, 1, set)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
	return ctx, span, done
}

// --- glue / management ---

func (t *otelStore) BeanName() string           { return t.delegate.BeanName() }
func (t *otelStore) Destroy() error             { return t.delegate.Destroy() }
func (t *otelStore) Features() store.Capability { return t.delegate.Features() }
func (t *otelStore) Instance() interface{}      { return t.delegate.Instance() }

func (t *otelStore) Compact(discardRatio float64) error {
	_, _, done := t.begin(context.Background(), "store.Compact")
	err := t.delegate.Compact(discardRatio)
	done(err)
	return err
}

func (t *otelStore) Backup(w io.Writer, since uint64) (uint64, error) {
	_, _, done := t.begin(context.Background(), "store.Backup")
	v, err := t.delegate.Backup(w, since)
	done(err)
	return v, err
}

func (t *otelStore) Restore(r io.Reader) error {
	_, _, done := t.begin(context.Background(), "store.Restore")
	err := t.delegate.Restore(r)
	done(err)
	return err
}

func (t *otelStore) DropAll() error {
	_, _, done := t.begin(context.Background(), "store.DropAll")
	err := t.delegate.DropAll()
	done(err)
	return err
}

func (t *otelStore) DropWithPrefix(prefix []byte) error {
	_, _, done := t.begin(context.Background(), "store.DropWithPrefix", attribute.String("store.prefix", string(prefix)))
	err := t.delegate.DropWithPrefix(prefix)
	done(err)
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

// --- raw operations (traced + metered) ---

func (t *otelStore) GetRaw(ctx context.Context, key []byte, ttlPtr *int, versionPtr *int64, required bool) ([]byte, error) {
	ctx, _, done := t.begin(ctx, "store.GetRaw", attribute.String("store.key", string(key)))
	val, err := t.delegate.GetRaw(ctx, key, ttlPtr, versionPtr, required)
	done(err)
	return val, err
}

func (t *otelStore) SetRaw(ctx context.Context, key, value []byte, ttlSeconds int) error {
	ctx, _, done := t.begin(ctx, "store.SetRaw",
		attribute.String("store.key", string(key)),
		attribute.Int("store.value_size", len(value)),
		attribute.Int("store.ttl", ttlSeconds))
	err := t.delegate.SetRaw(ctx, key, value, ttlSeconds)
	done(err)
	return err
}

func (t *otelStore) SetBatchRaw(ctx context.Context, entries []store.RawEntry) error {
	ctx, _, done := t.begin(ctx, "store.SetBatchRaw", attribute.Int("store.batch_size", len(entries)))
	err := t.delegate.SetBatchRaw(ctx, entries)
	done(err)
	return err
}

func (t *otelStore) CompareAndSetRaw(ctx context.Context, key, value []byte, ttlSeconds int, version int64) (bool, error) {
	ctx, span, done := t.begin(ctx, "store.CompareAndSetRaw",
		attribute.String("store.key", string(key)),
		attribute.Int64("store.version", version))
	ok, err := t.delegate.CompareAndSetRaw(ctx, key, value, ttlSeconds, version)
	span.SetAttributes(attribute.Bool("store.updated", ok))
	done(err)
	return ok, err
}

func (t *otelStore) IncrementRaw(ctx context.Context, key []byte, initial, delta int64, ttlSeconds int) (int64, error) {
	ctx, _, done := t.begin(ctx, "store.IncrementRaw",
		attribute.String("store.key", string(key)),
		attribute.Int64("store.delta", delta))
	prev, err := t.delegate.IncrementRaw(ctx, key, initial, delta, ttlSeconds)
	done(err)
	return prev, err
}

func (t *otelStore) TouchRaw(ctx context.Context, key []byte, ttlSeconds int) error {
	ctx, _, done := t.begin(ctx, "store.TouchRaw",
		attribute.String("store.key", string(key)),
		attribute.Int("store.ttl", ttlSeconds))
	err := t.delegate.TouchRaw(ctx, key, ttlSeconds)
	done(err)
	return err
}

func (t *otelStore) RemoveRaw(ctx context.Context, key []byte) error {
	ctx, _, done := t.begin(ctx, "store.RemoveRaw", attribute.String("store.key", string(key)))
	err := t.delegate.RemoveRaw(ctx, key)
	done(err)
	return err
}

func (t *otelStore) EnumerateRaw(ctx context.Context, prefix, seek []byte, batchSize int, onlyKeys bool, reverse bool, cb func(*store.RawEntry) bool) error {
	ctx, _, done := t.begin(ctx, "store.EnumerateRaw",
		attribute.String("store.prefix", string(prefix)),
		attribute.Bool("store.only_keys", onlyKeys),
		attribute.Bool("store.reverse", reverse))
	err := t.delegate.EnumerateRaw(ctx, prefix, seek, batchSize, onlyKeys, reverse, cb)
	done(err)
	return err
}

func (t *otelStore) WatchRaw(ctx context.Context, prefix []byte, cb func(*store.WatchEvent) bool) error {
	ctx, _, done := t.begin(ctx, "store.WatchRaw", attribute.String("store.prefix", string(prefix)))
	err := t.delegate.WatchRaw(ctx, prefix, cb)
	done(err)
	return err
}
