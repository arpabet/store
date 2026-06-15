/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package compress_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
	"go.arpabet.com/store"
	"go.arpabet.com/store/middleware/compress"
	cryptostore "go.arpabet.com/store/middleware/crypto"
	memstore "go.arpabet.com/store/providers/mem"
	"go.arpabet.com/store/storetest"
)

func TestConformanceZstd(t *testing.T) {
	storetest.RunConformance(t, func(t *testing.T) store.ManagedDataStore {
		delegate := memstore.New("conf")
		s, err := compress.New(delegate.Interface())
		require.NoError(t, err)
		t.Cleanup(func() { delegate.Destroy() })
		return s
	})
}

func TestConformanceSnappy(t *testing.T) {
	storetest.RunConformance(t, func(t *testing.T) store.ManagedDataStore {
		delegate := memstore.New("conf")
		s, err := compress.New(delegate.Interface(), compress.WithSnappy())
		require.NoError(t, err)
		t.Cleanup(func() { delegate.Destroy() })
		return s
	})
}

// A large, repetitive value must be smaller at rest and round-trip cleanly.
func TestCompressesAtRest(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []compress.Option
		want compress.Codec
	}{
		{"zstd", nil, compress.Zstd},
		{"snappy", []compress.Option{compress.WithSnappy()}, compress.Snappy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			delegate := memstore.New("c")
			defer delegate.Destroy()
			s, err := compress.New(delegate.Interface(), tc.opts...)
			require.NoError(t, err)
			ctx := context.Background()

			value := bytes.Repeat([]byte("the quick brown fox "), 1000) // ~20KB, very compressible
			require.NoError(t, s.Set(ctx).ByKey("k:big").Binary(value))

			atRest, err := delegate.Interface().Get(ctx).ByKey("k:big").ToBinary()
			require.NoError(t, err)
			require.Equal(t, byte(tc.want), atRest[0], "codec marker")
			require.Less(t, len(atRest), len(value)/2, "should compress well below half")

			got, err := s.Get(ctx).ByKey("k:big").ToBinary()
			require.NoError(t, err)
			require.Equal(t, value, got)
		})
	}
}

// Tiny / incompressible values are stored uncompressed (None marker), never inflated.
func TestNoneFallback(t *testing.T) {
	delegate := memstore.New("c")
	defer delegate.Destroy()
	s, err := compress.New(delegate.Interface(), compress.WithMinSize(0)) // force a compression attempt
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx).ByKey("k:small").String("hi"))
	atRest, err := delegate.Interface().Get(ctx).ByKey("k:small").ToBinary()
	require.NoError(t, err)
	require.Equal(t, byte(compress.None), atRest[0])
	require.LessOrEqual(t, len(atRest), 1+len("hi")) // marker + raw, no inflation

	got, err := s.Get(ctx).ByKey("k:small").ToString()
	require.NoError(t, err)
	require.Equal(t, "hi", got)
}

// compress(crypto(base)): plaintext compresses, then is encrypted at rest.
func TestComposeWithCrypto(t *testing.T) {
	delegate := memstore.New("cc")
	defer delegate.Destroy()
	enc, err := cryptostore.New(delegate.Interface(), bytes.Repeat([]byte("k"), 32))
	require.NoError(t, err)
	s, err := compress.New(enc, compress.WithZstd(zstd.SpeedDefault))
	require.NoError(t, err)

	ctx := context.Background()
	value := bytes.Repeat([]byte("SENSITIVE-"), 500)
	require.NoError(t, s.Set(ctx).ByKey("k:doc").Binary(value))

	got, err := s.Get(ctx).ByKey("k:doc").ToBinary()
	require.NoError(t, err)
	require.Equal(t, value, got)

	// at rest the plaintext must not appear (encrypted on top of compression)
	atRest, err := delegate.Interface().Get(ctx).ByKey("k:doc").ToBinary()
	require.NoError(t, err)
	require.False(t, bytes.Contains(atRest, []byte("SENSITIVE")))
}
