/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package otelstore_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	otelstore "go.arpabet.com/store/middleware/otel"
	memstore "go.arpabet.com/store/providers/mem"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func sumInt64(rm metricdata.ResourceMetrics, name string) int64 {
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
				for _, dp := range sum.DataPoints {
					total += dp.Value
				}
			}
		}
	}
	return total
}

func histogramCount(rm metricdata.ResourceMetrics, name string) uint64 {
	var total uint64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			if h, ok := m.Data.(metricdata.Histogram[float64]); ok {
				for _, dp := range h.DataPoints {
					total += dp.Count
				}
			}
		}
	}
	return total
}

func TestMetricsRecorded(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	delegate := memstore.New("metrics")
	defer delegate.Destroy()
	s := otelstore.New(delegate.Interface(), otelstore.WithMeterProvider(mp))

	ctx := context.Background()
	require.NoError(t, s.Set(ctx).ByKey("k:1").String("v"))           // 1 op
	_, err := s.Get(ctx).ByKey("k:1").ToString()                     // 2 ops
	require.NoError(t, err)
	_, err = s.Get(ctx).ByKey("k:missing").Required().ToString()     // 3 ops, 1 error
	require.Error(t, err)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))

	require.GreaterOrEqual(t, sumInt64(rm, "store.operations"), int64(3), "operation counter")
	require.GreaterOrEqual(t, sumInt64(rm, "store.errors"), int64(1), "error counter")
	require.GreaterOrEqual(t, histogramCount(rm, "store.operation.duration"), uint64(3), "latency histogram")
}
