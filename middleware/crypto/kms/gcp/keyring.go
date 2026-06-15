/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

// Package gcpkms implements cryptostore.Keyring backed by Google Cloud
// KMS envelope encryption. cryptostore's symmetric data keys are never stored in
// plaintext: a fresh AES-256 key is generated locally and wrapped via the KMS
// Encrypt API; the wrapped blob is what gets persisted, and KMS Decrypt unwraps
// it on demand. The Cloud KMS crypto key never leaves Google's HSM/software backend.
//
// Unlike AWS KMS there is no GenerateDataKey call, so the data key is created
// here and handed to KMS to wrap — the same approach the age adapter uses.
//
// Rotation is online: Generate a new wrapped data key, SetActive to it; existing
// values keep decrypting under their old key id.
package gcpkms

import (
	"context"
	"crypto/rand"
	"io"
	"sync"

	kmspb "cloud.google.com/go/kms/apiv1/kmspb"
	gax "github.com/googleapis/gax-go/v2"
	cryptostore "go.arpabet.com/store/middleware/crypto"
)

// compile-time guarantee that this satisfies the cryptostore.Keyring contract.
var _ cryptostore.Keyring = (*Keyring)(nil)

const dataKeySize = 32 // AES-256

// Client is the subset of the Cloud KMS API used by the keyring. The concrete
// *kms.KeyManagementClient from cloud.google.com/go/kms/apiv1 satisfies it; tests
// can supply a fake.
type Client interface {
	Encrypt(ctx context.Context, req *kmspb.EncryptRequest, opts ...gax.CallOption) (*kmspb.EncryptResponse, error)
	Decrypt(ctx context.Context, req *kmspb.DecryptRequest, opts ...gax.CallOption) (*kmspb.DecryptResponse, error)
}

type Option func(*Keyring)

// WithContext sets the context used for KMS calls (default context.Background()).
func WithContext(ctx context.Context) Option {
	return func(t *Keyring) { t.ctx = ctx }
}

// WithAAD sets the additional authenticated data applied to both wrap and unwrap.
// It must match for decryption to succeed.
func WithAAD(aad []byte) Option {
	return func(t *Keyring) { t.aad = aad }
}

// Keyring wraps/unwraps cryptostore data keys with Cloud KMS. Safe for concurrent use.
type Keyring struct {
	client  Client
	keyName string // full crypto key resource name: projects/.../cryptoKeys/...
	ctx     context.Context
	aad     []byte

	mu        sync.RWMutex
	wrapped   map[uint32][]byte // id -> KMS-encrypted data key (persistable)
	plain     map[uint32][]byte // id -> unwrapped data key (cache)
	active    uint32
	hasActive bool
}

// New creates a keyring that wraps data keys with the Cloud KMS crypto key
// identified by keyName (projects/P/locations/L/keyRings/R/cryptoKeys/K).
func New(client Client, keyName string, opts ...Option) *Keyring {
	t := &Keyring{
		client:  client,
		keyName: keyName,
		ctx:     context.Background(),
		wrapped: make(map[uint32][]byte),
		plain:   make(map[uint32][]byte),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Generate creates a fresh random data key, wraps it via KMS Encrypt, stores it
// (active if it is the first key), and returns the wrapped blob so the caller can
// persist it for later AddWrapped.
func (t *Keyring) Generate(id uint32) ([]byte, error) {
	dataKey := make([]byte, dataKeySize)
	if _, err := io.ReadFull(rand.Reader, dataKey); err != nil {
		return nil, err
	}
	resp, err := t.client.Encrypt(t.ctx, &kmspb.EncryptRequest{
		Name:                        t.keyName,
		Plaintext:                   dataKey,
		AdditionalAuthenticatedData: t.aad,
	})
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.wrapped[id] = resp.Ciphertext
	t.plain[id] = dataKey
	if !t.hasActive {
		t.active = id
		t.hasActive = true
	}
	t.mu.Unlock()
	return resp.Ciphertext, nil
}

// AddWrapped registers a previously persisted wrapped data key under id (active
// if it is the first key). It is unwrapped lazily on first use.
func (t *Keyring) AddWrapped(id uint32, blob []byte) {
	dup := make([]byte, len(blob))
	copy(dup, blob)
	t.mu.Lock()
	t.wrapped[id] = dup
	if !t.hasActive {
		t.active = id
		t.hasActive = true
	}
	t.mu.Unlock()
}

// SetActive selects which key id seals new writes (the rotation point).
func (t *Keyring) SetActive(id uint32) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, w := t.wrapped[id]
	_, p := t.plain[id]
	if !w && !p {
		return cryptostore.ErrUnknownKeyID
	}
	t.active = id
	t.hasActive = true
	return nil
}

// Wrapped returns a copy of the persistable wrapped blob for id.
func (t *Keyring) Wrapped(id uint32) ([]byte, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	blob, ok := t.wrapped[id]
	if !ok {
		return nil, cryptostore.ErrUnknownKeyID
	}
	dup := make([]byte, len(blob))
	copy(dup, blob)
	return dup, nil
}

// Active implements cryptostore.Keyring.
func (t *Keyring) Active() (uint32, []byte, error) {
	t.mu.RLock()
	active, has := t.active, t.hasActive
	t.mu.RUnlock()
	if !has {
		return 0, nil, cryptostore.ErrNoActiveKey
	}
	key, err := t.Get(active)
	return active, key, err
}

// Get implements cryptostore.Keyring, calling KMS Decrypt (and caching) on demand.
func (t *Keyring) Get(id uint32) ([]byte, error) {
	t.mu.RLock()
	if key, ok := t.plain[id]; ok {
		t.mu.RUnlock()
		return key, nil
	}
	blob, ok := t.wrapped[id]
	t.mu.RUnlock()
	if !ok {
		return nil, cryptostore.ErrUnknownKeyID
	}

	resp, err := t.client.Decrypt(t.ctx, &kmspb.DecryptRequest{
		Name:                        t.keyName,
		Ciphertext:                  blob,
		AdditionalAuthenticatedData: t.aad,
	})
	if err != nil {
		return nil, err
	}

	t.mu.Lock()
	t.plain[id] = resp.Plaintext
	t.mu.Unlock()
	return resp.Plaintext, nil
}
