/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

// Package awskms implements cryptostore.Keyring backed by AWS KMS envelope
// encryption. cryptostore's symmetric data keys are never stored in plaintext:
// KMS GenerateDataKey returns a fresh data key together with a KMS-encrypted
// ("wrapped") copy, which is what gets persisted; KMS Decrypt unwraps it on
// demand. The KMS customer master key never leaves KMS.
//
// Rotation is online: Generate a new data key under the same (or a new) KMS key,
// SetActive to it; existing values keep decrypting under their old key id.
package awskms

import (
	"context"
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
	cryptostore "go.arpabet.com/store/middleware/crypto"
)

// compile-time guarantee that this satisfies the cryptostore.Keyring contract.
var _ cryptostore.Keyring = (*Keyring)(nil)

// Client is the subset of the AWS KMS API used by the keyring. The concrete
// *kms.Client from aws-sdk-go-v2 satisfies it; tests can supply a fake.
type Client interface {
	GenerateDataKey(ctx context.Context, params *kms.GenerateDataKeyInput, optFns ...func(*kms.Options)) (*kms.GenerateDataKeyOutput, error)
	Decrypt(ctx context.Context, params *kms.DecryptInput, optFns ...func(*kms.Options)) (*kms.DecryptOutput, error)
}

type Option func(*Keyring)

// WithContext sets the context used for KMS calls (default context.Background()).
func WithContext(ctx context.Context) Option {
	return func(t *Keyring) { t.ctx = ctx }
}

// WithEncryptionContext sets the KMS encryption context (additional authenticated
// data) applied to both wrap and unwrap. It must match for decryption to succeed.
func WithEncryptionContext(ec map[string]string) Option {
	return func(t *Keyring) { t.encryptionContext = ec }
}

// Keyring wraps/unwraps cryptostore data keys with AWS KMS. Safe for concurrent use.
type Keyring struct {
	client            Client
	keyID             string // KMS key id, ARN, or alias used by GenerateDataKey
	ctx               context.Context
	encryptionContext map[string]string

	mu        sync.RWMutex
	wrapped   map[uint32][]byte // id -> KMS-encrypted data key (persistable)
	plain     map[uint32][]byte // id -> unwrapped data key (cache)
	active    uint32
	hasActive bool
}

// New creates a keyring that generates new data keys under keyID (an id, ARN, or
// alias such as "alias/my-key").
func New(client Client, keyID string, opts ...Option) *Keyring {
	t := &Keyring{
		client:  client,
		keyID:   keyID,
		ctx:     context.Background(),
		wrapped: make(map[uint32][]byte),
		plain:   make(map[uint32][]byte),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Generate asks KMS for a fresh AES-256 data key, stores the plaintext (cache)
// and the KMS-encrypted blob (active if it is the first key), and returns the
// wrapped blob so the caller can persist it for later AddWrapped.
func (t *Keyring) Generate(id uint32) ([]byte, error) {
	out, err := t.client.GenerateDataKey(t.ctx, &kms.GenerateDataKeyInput{
		KeyId:             &t.keyID,
		KeySpec:           types.DataKeySpecAes256,
		EncryptionContext: t.encryptionContext,
	})
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.wrapped[id] = out.CiphertextBlob
	t.plain[id] = out.Plaintext
	if !t.hasActive {
		t.active = id
		t.hasActive = true
	}
	t.mu.Unlock()
	return out.CiphertextBlob, nil
}

// AddWrapped registers a previously persisted KMS-encrypted data key under id
// (active if it is the first key). It is unwrapped lazily on first use.
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

// Wrapped returns a copy of the persistable KMS-encrypted blob for id.
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

	out, err := t.client.Decrypt(t.ctx, &kms.DecryptInput{
		CiphertextBlob:    blob,
		KeyId:             &t.keyID,
		EncryptionContext: t.encryptionContext,
	})
	if err != nil {
		return nil, err
	}

	t.mu.Lock()
	t.plain[id] = out.Plaintext
	t.mu.Unlock()
	return out.Plaintext, nil
}
