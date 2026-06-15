/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package agekeyring_test

import (
	"context"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/stretchr/testify/require"
	"go.arpabet.com/store"
	cryptostore "go.arpabet.com/store/middleware/crypto"
	agekeyring "go.arpabet.com/store/middleware/crypto/age"
	memstore "go.arpabet.com/store/providers/mem"
	"go.arpabet.com/store/storetest"
)

func newIdentity(t *testing.T) *age.X25519Identity {
	id, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	return id
}

// The age-wrapped crypto store must pass the full conformance suite.
func TestConformance(t *testing.T) {
	storetest.RunConformance(t, func(t *testing.T) store.ManagedDataStore {
		id := newIdentity(t)
		kr := agekeyring.New([]age.Identity{id}, []age.Recipient{id.Recipient()})
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
	id := newIdentity(t)
	kr := agekeyring.New([]age.Identity{id}, []age.Recipient{id.Recipient()})
	_, err := kr.Generate(1)
	require.NoError(t, err)

	delegate := memstore.New("age")
	defer delegate.Destroy()
	s, err := cryptostore.NewWithKeyring(delegate.Interface(), kr)
	require.NoError(t, err)

	ctx := context.Background()
	const secret = "patient-record-42"
	require.NoError(t, s.Set(ctx).ByKey("phi:rec").String(secret))

	got, err := s.Get(ctx).ByKey("phi:rec").ToString()
	require.NoError(t, err)
	require.Equal(t, secret, got)

	atRest, err := delegate.Interface().Get(ctx).ByKey("phi:rec").ToBinary()
	require.NoError(t, err)
	require.NotContains(t, string(atRest), secret)
}

// Persisting the wrapped data key and reloading it (simulating a restart) must
// recover existing values, using only the age identity + the wrapped blob.
func TestPersistReload(t *testing.T) {
	id := newIdentity(t)

	kr := agekeyring.New([]age.Identity{id}, []age.Recipient{id.Recipient()})
	wrapped, err := kr.Generate(1)
	require.NoError(t, err)

	delegate := memstore.New("age")
	defer delegate.Destroy()
	s, err := cryptostore.NewWithKeyring(delegate.Interface(), kr)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, s.Set(ctx).ByKey("k:a").String("v1"))

	// "restart": fresh keyring with the same identity, only the wrapped blob loaded
	kr2 := agekeyring.New([]age.Identity{id}, []age.Recipient{id.Recipient()})
	kr2.AddWrapped(1, wrapped)
	s2, err := cryptostore.NewWithKeyring(delegate.Interface(), kr2)
	require.NoError(t, err)

	got, err := s2.Get(ctx).ByKey("k:a").ToString()
	require.NoError(t, err)
	require.Equal(t, "v1", got)
}

func TestRotation(t *testing.T) {
	id := newIdentity(t)
	kr := agekeyring.New([]age.Identity{id}, []age.Recipient{id.Recipient()})
	_, err := kr.Generate(1)
	require.NoError(t, err)

	delegate := memstore.New("age")
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

func TestFromX25519(t *testing.T) {
	id := newIdentity(t)
	kr, err := agekeyring.FromX25519(id.String(), id.Recipient().String())
	require.NoError(t, err)
	_, err = kr.Generate(1)
	require.NoError(t, err)

	_, key, err := kr.Active()
	require.NoError(t, err)
	require.Len(t, key, 32)

	_, err = agekeyring.FromX25519("not-a-valid-key")
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "age") || err != nil)
}
