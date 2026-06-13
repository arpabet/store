/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package benchmarks_test

import (
	"context"
	"fmt"
	"testing"

	cryptostore "go.arpabet.com/store/middleware/crypto"
	memstore "go.arpabet.com/store/providers/mem"
)

var (
	rotKey1 = []byte("0123456789abcdef0123456789abcdef") // 32 bytes -> AES-256
	rotKey2 = []byte("ABCDEF0123456789ABCDEF0123456789")
)

// BenchmarkRotation measures the post-rotation hot paths of the crypto
// middleware: reading values that were sealed under a now-historical key (the
// key id is resolved through the keyring and the AEAD is cached) and writing new
// values under the rotated-in active key.
func BenchmarkRotation(b *testing.B) {
	const n = 10000
	ctx := context.Background()

	delegate := memstore.New("rotbench")
	b.Cleanup(func() { delegate.Destroy() })

	kr := cryptostore.NewStaticKeyring().Add(1, rotKey1) // id 1 active
	s, err := cryptostore.NewWithKeyring(delegate.Interface(), kr)
	if err != nil {
		b.Fatal(err)
	}

	val := make([]byte, 128)
	for i := 0; i < n; i++ {
		if err := s.SetRaw(ctx, []byte(fmt.Sprintf("rot:%08d", i)), val, 0); err != nil {
			b.Fatal(err)
		}
	}

	// rotate: id 2 becomes active; the n values above stay sealed under id 1
	kr.Add(2, rotKey2)
	if err := kr.SetActive(2); err != nil {
		b.Fatal(err)
	}

	b.Run("ReadHistoricalKey", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := s.GetRaw(ctx, []byte(fmt.Sprintf("rot:%08d", i%n)), nil, nil, false); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("WriteActiveKey", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := s.SetRaw(ctx, []byte(fmt.Sprintf("rotnew:%08d", i)), val, 0); err != nil {
				b.Fatal(err)
			}
		}
	})
}
