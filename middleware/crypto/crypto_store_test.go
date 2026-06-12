/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package cryptostore_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.arpabet.com/store"
	cryptostore "go.arpabet.com/store/middleware/crypto"
	memstore "go.arpabet.com/store/providers/mem"
	"go.arpabet.com/store/storetest"
)

var testKey = []byte("0123456789abcdef0123456789abcdef") // 32 bytes -> AES-256

func TestConformance(t *testing.T) {
	storetest.RunConformance(t, func(t *testing.T) store.ManagedDataStore {
		delegate := memstore.New("conf")
		s, err := cryptostore.New(delegate.Interface(), testKey)
		require.NoError(t, err)
		t.Cleanup(func() { delegate.Destroy() })
		return s
	})
}

func TestEncryptedAtRest(t *testing.T) {
	delegate := memstore.New("conf")
	defer delegate.Destroy()
	s, err := cryptostore.New(delegate.Interface(), testKey)
	require.NoError(t, err)

	require.True(t, s.Features().Has(store.EncryptedCapability))

	ctx := context.Background()
	secret := "social-security-number-12345"
	require.NoError(t, s.Set(ctx).ByKey("pii:ssn").String(secret))

	// the underlying store must hold ciphertext, not the plaintext
	atRest, err := delegate.Interface().Get(ctx).ByKey("pii:ssn").ToBinary()
	require.NoError(t, err)
	require.NotContains(t, string(atRest), secret)
	require.False(t, bytes.Contains(atRest, []byte(secret)))

	// reading through the crypto layer returns the plaintext
	got, err := s.Get(ctx).ByKey("pii:ssn").ToString()
	require.NoError(t, err)
	require.Equal(t, secret, got)
}
