/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package kmskeyring_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/stretchr/testify/require"
	"go.arpabet.com/store"
	cryptostore "go.arpabet.com/store/middleware/crypto"
	kmskeyring "go.arpabet.com/store/middleware/crypto/kms"
	memstore "go.arpabet.com/store/providers/mem"
	"go.arpabet.com/store/storetest"
)

// fakeKMS is a stand-in for AWS KMS that needs no credentials. GenerateDataKey
// returns a real random 32-byte key; the "ciphertext blob" is a reversible
// envelope (prefix + plaintext) so any instance can Decrypt it — which also lets
// the persist/reload test use a brand new client.
type fakeKMS struct{ keyID string }

var fakePrefix = []byte("FAKEKMS:")

func (f *fakeKMS) GenerateDataKey(ctx context.Context, in *kms.GenerateDataKeyInput, _ ...func(*kms.Options)) (*kms.GenerateDataKeyOutput, error) {
	if in.KeyId == nil || *in.KeyId == "" {
		return nil, errors.New("fakekms: missing KeyId")
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	blob := append(append([]byte{}, fakePrefix...), key...)
	return &kms.GenerateDataKeyOutput{Plaintext: key, CiphertextBlob: blob, KeyId: in.KeyId}, nil
}

func (f *fakeKMS) Decrypt(ctx context.Context, in *kms.DecryptInput, _ ...func(*kms.Options)) (*kms.DecryptOutput, error) {
	if !bytes.HasPrefix(in.CiphertextBlob, fakePrefix) {
		return nil, errors.New("fakekms: invalid ciphertext blob")
	}
	return &kms.DecryptOutput{Plaintext: in.CiphertextBlob[len(fakePrefix):], KeyId: in.KeyId}, nil
}

const testKeyID = "alias/store-test"

// The KMS-wrapped crypto store must pass the full conformance suite.
func TestConformance(t *testing.T) {
	storetest.RunConformance(t, func(t *testing.T) store.ManagedDataStore {
		kr := kmskeyring.New(&fakeKMS{}, testKeyID)
		_, err := kr.Generate(1)
		require.NoError(t, err)
		delegate := memstore.New("conf")
		s, err := cryptostore.NewWithKeyring(delegate.Interface(), kr)
		require.NoError(t, err)
		t.Cleanup(func() { delegate.Destroy() })
		return s
	})
}

func TestRoundTripAndAtRest(t *testing.T) {
	kr := kmskeyring.New(&fakeKMS{}, testKeyID)
	_, err := kr.Generate(1)
	require.NoError(t, err)

	delegate := memstore.New("kms")
	defer delegate.Destroy()
	s, err := cryptostore.NewWithKeyring(delegate.Interface(), kr)
	require.NoError(t, err)

	ctx := context.Background()
	const secret = "card-number-4111111111111111"
	require.NoError(t, s.Set(ctx).ByKey("pci:card").String(secret))

	got, err := s.Get(ctx).ByKey("pci:card").ToString()
	require.NoError(t, err)
	require.Equal(t, secret, got)

	atRest, err := delegate.Interface().Get(ctx).ByKey("pci:card").ToBinary()
	require.NoError(t, err)
	require.NotContains(t, string(atRest), secret)
}

// Persisting the KMS-wrapped data key and reloading it with a fresh client/keyring
// (simulating a restart) must recover existing values.
func TestPersistReload(t *testing.T) {
	kr := kmskeyring.New(&fakeKMS{}, testKeyID)
	wrapped, err := kr.Generate(1)
	require.NoError(t, err)

	delegate := memstore.New("kms")
	defer delegate.Destroy()
	s, err := cryptostore.NewWithKeyring(delegate.Interface(), kr)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, s.Set(ctx).ByKey("k:a").String("v1"))

	// "restart": new client + keyring, only the wrapped blob loaded
	kr2 := kmskeyring.New(&fakeKMS{}, testKeyID)
	kr2.AddWrapped(1, wrapped)
	s2, err := cryptostore.NewWithKeyring(delegate.Interface(), kr2)
	require.NoError(t, err)

	got, err := s2.Get(ctx).ByKey("k:a").ToString()
	require.NoError(t, err)
	require.Equal(t, "v1", got)
}

func TestRotation(t *testing.T) {
	kr := kmskeyring.New(&fakeKMS{}, testKeyID)
	_, err := kr.Generate(1)
	require.NoError(t, err)

	delegate := memstore.New("kms")
	defer delegate.Destroy()
	s, err := cryptostore.NewWithKeyring(delegate.Interface(), kr)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, s.Set(ctx).ByKey("k:old").String("old"))

	_, err = kr.Generate(2)
	require.NoError(t, err)
	require.NoError(t, kr.SetActive(2))
	require.NoError(t, s.Set(ctx).ByKey("k:new").String("new"))

	old, err := s.Get(ctx).ByKey("k:old").ToString()
	require.NoError(t, err)
	require.Equal(t, "old", old)
	got, err := s.Get(ctx).ByKey("k:new").ToString()
	require.NoError(t, err)
	require.Equal(t, "new", got)
}
