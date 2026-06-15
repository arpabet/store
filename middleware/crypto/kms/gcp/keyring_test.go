/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package gcpkms_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	kmspb "cloud.google.com/go/kms/apiv1/kmspb"
	gax "github.com/googleapis/gax-go/v2"
	"github.com/stretchr/testify/require"
	"go.arpabet.com/store"
	cryptostore "go.arpabet.com/store/middleware/crypto"
	gcpkms "go.arpabet.com/store/middleware/crypto/kms/gcp"
	memstore "go.arpabet.com/store/providers/mem"
	"go.arpabet.com/store/storetest"
)

// fakeKMS is a stand-in for Cloud KMS that needs no credentials. The "ciphertext"
// is a reversible envelope (prefix + plaintext) so any instance can Decrypt it,
// which lets the persist/reload test use a brand new client.
type fakeKMS struct{}

var fakePrefix = []byte("FAKEGCP:")

func (f *fakeKMS) Encrypt(ctx context.Context, req *kmspb.EncryptRequest, _ ...gax.CallOption) (*kmspb.EncryptResponse, error) {
	if req.Name == "" {
		return nil, errors.New("fakekms: missing Name")
	}
	ct := append(append([]byte{}, fakePrefix...), req.Plaintext...)
	return &kmspb.EncryptResponse{Name: req.Name, Ciphertext: ct}, nil
}

func (f *fakeKMS) Decrypt(ctx context.Context, req *kmspb.DecryptRequest, _ ...gax.CallOption) (*kmspb.DecryptResponse, error) {
	if !bytes.HasPrefix(req.Ciphertext, fakePrefix) {
		return nil, errors.New("fakekms: invalid ciphertext")
	}
	return &kmspb.DecryptResponse{Plaintext: req.Ciphertext[len(fakePrefix):]}, nil
}

const testKeyName = "projects/p/locations/global/keyRings/r/cryptoKeys/k"

// The GCP-KMS-wrapped crypto store must pass the full conformance suite.
func TestConformance(t *testing.T) {
	storetest.RunConformance(t, func(t *testing.T) store.ManagedDataStore {
		kr := gcpkms.New(&fakeKMS{}, testKeyName)
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
	kr := gcpkms.New(&fakeKMS{}, testKeyName)
	_, err := kr.Generate(1)
	require.NoError(t, err)

	delegate := memstore.New("gcpkms")
	defer delegate.Destroy()
	s, err := cryptostore.NewWithKeyring(delegate.Interface(), kr)
	require.NoError(t, err)

	ctx := context.Background()
	const secret = "national-id-AB1234567"
	require.NoError(t, s.Set(ctx).ByKey("pii:nid").String(secret))

	got, err := s.Get(ctx).ByKey("pii:nid").ToString()
	require.NoError(t, err)
	require.Equal(t, secret, got)

	atRest, err := delegate.Interface().Get(ctx).ByKey("pii:nid").ToBinary()
	require.NoError(t, err)
	require.NotContains(t, string(atRest), secret)
}

// Persisting the wrapped data key and reloading it with a fresh client/keyring
// (simulating a restart) must recover existing values.
func TestPersistReload(t *testing.T) {
	kr := gcpkms.New(&fakeKMS{}, testKeyName)
	wrapped, err := kr.Generate(1)
	require.NoError(t, err)

	delegate := memstore.New("gcpkms")
	defer delegate.Destroy()
	s, err := cryptostore.NewWithKeyring(delegate.Interface(), kr)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, s.Set(ctx).ByKey("k:a").String("v1"))

	// "restart": new client + keyring, only the wrapped blob loaded
	kr2 := gcpkms.New(&fakeKMS{}, testKeyName)
	kr2.AddWrapped(1, wrapped)
	s2, err := cryptostore.NewWithKeyring(delegate.Interface(), kr2)
	require.NoError(t, err)

	got, err := s2.Get(ctx).ByKey("k:a").ToString()
	require.NoError(t, err)
	require.Equal(t, "v1", got)
}

func TestRotation(t *testing.T) {
	kr := gcpkms.New(&fakeKMS{}, testKeyName)
	_, err := kr.Generate(1)
	require.NoError(t, err)

	delegate := memstore.New("gcpkms")
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
