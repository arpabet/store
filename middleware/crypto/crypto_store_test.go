/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package cryptostore_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
	"go.arpabet.com/store"
	cryptostore "go.arpabet.com/store/middleware/crypto"
	memstore "go.arpabet.com/store/providers/mem"
	"go.arpabet.com/store/storetest"
)

var testKey = []byte("0123456789abcdef0123456789abcdef") // 32 bytes -> AES-256
var testKey2 = []byte("ABCDEF0123456789ABCDEF0123456789") // second 32-byte key

// keyIDAtRest returns the key id encoded in the first 4 bytes of the stored value.
func keyIDAtRest(t *testing.T, delegate store.ManagedDataStore, key string) uint32 {
	atRest, err := delegate.Get(context.Background()).ByKey("%s", key).ToBinary()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(atRest), 4)
	return binary.BigEndian.Uint32(atRest[:4])
}

func TestKeyRotation(t *testing.T) {
	delegate := memstore.New("rot")
	defer delegate.Destroy()

	kr := cryptostore.NewStaticKeyring().Add(1, testKey) // id 1 active
	s, err := cryptostore.NewWithKeyring(delegate.Interface(), kr)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, s.Set(ctx).ByKey("k:old").String("old-value"))
	require.Equal(t, uint32(1), keyIDAtRest(t, delegate.Interface(), "k:old"))

	// rotate: add a new key and make it active (online; same store instance)
	kr.Add(2, testKey2)
	require.NoError(t, kr.SetActive(2))

	require.NoError(t, s.Set(ctx).ByKey("k:new").String("new-value"))
	require.Equal(t, uint32(2), keyIDAtRest(t, delegate.Interface(), "k:new"))

	// old value still decrypts (resolved to key id 1); new value uses key id 2
	old, err := s.Get(ctx).ByKey("k:old").ToString()
	require.NoError(t, err)
	require.Equal(t, "old-value", old)

	got, err := s.Get(ctx).ByKey("k:new").ToString()
	require.NoError(t, err)
	require.Equal(t, "new-value", got)

	// overwriting an old key re-seals it under the active key
	require.NoError(t, s.Set(ctx).ByKey("k:old").String("rewritten"))
	require.Equal(t, uint32(2), keyIDAtRest(t, delegate.Interface(), "k:old"))
}

func TestConformance(t *testing.T) {
	storetest.RunConformance(t, func(t *testing.T) store.ManagedDataStore {
		delegate := memstore.New("conf")
		s, err := cryptostore.New(delegate.Interface(), testKey)
		require.NoError(t, err)
		t.Cleanup(func() { delegate.Destroy() })
		return s
	})
}

// TestRotationConformance runs the shared rotation suite, rotating the keyring's
// active key from id 1 to id 2 as the rotate hook.
func TestRotationConformance(t *testing.T) {
	storetest.RunRotation(t, func(t *testing.T) (store.ManagedDataStore, func() error) {
		delegate := memstore.New("rotconf")
		kr := cryptostore.NewStaticKeyring().Add(1, testKey)
		s, err := cryptostore.NewWithKeyring(delegate.Interface(), kr)
		require.NoError(t, err)
		t.Cleanup(func() { delegate.Destroy() })
		rotate := func() error {
			kr.Add(2, testKey2)
			return kr.SetActive(2)
		}
		return s, rotate
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
