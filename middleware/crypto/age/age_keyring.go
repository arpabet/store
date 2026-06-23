/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

// Package agekeyring implements cryptostore.Keyring backed by age
// (https://filippo.io/age). The symmetric data keys that cryptostore uses to
// seal values are themselves never stored in plaintext: each is a random AES-256
// key wrapped (encrypted) to one or more age recipients, and unwrapped on demand
// with the configured age identities (which may be X25519 keys, an scrypt
// passphrase, an SSH key, or a hardware token via an age plugin).
//
// Rotation is online: Generate a new wrapped data key, SetActive to it; existing
// values keep decrypting under their old (still-loaded) key id.
package agekeyring

import (
	"bytes"
	"crypto/rand"
	"golang.org/x/xerrors"
	"io"
	"sync"

	"filippo.io/age"
	cryptostore "go.arpabet.com/store/middleware/crypto"
)

// compile-time guarantee that this satisfies the cryptostore.Keyring contract.
var _ cryptostore.Keyring = (*Keyring)(nil)

const dataKeySize = 32 // AES-256

var (
	ErrNoRecipients = xerrors.New("agekeyring: no recipients to wrap data key")
	ErrNoIdentities = xerrors.New("agekeyring: no identities to unwrap data key")
)

// Keyring wraps/unwraps cryptostore data keys with age. It is safe for
// concurrent use.
type Keyring struct {
	identities []age.Identity
	recipients []age.Recipient

	mu        sync.RWMutex
	wrapped   map[uint32][]byte // id -> age-wrapped data key (persistable)
	plain     map[uint32][]byte // id -> unwrapped data key (cache)
	active    uint32
	hasActive bool
}

// New creates a keyring that unwraps with identities and wraps new keys to
// recipients.
func New(identities []age.Identity, recipients []age.Recipient) *Keyring {
	return &Keyring{
		identities: identities,
		recipients: recipients,
		wrapped:    make(map[uint32][]byte),
		plain:      make(map[uint32][]byte),
	}
}

// FromX25519 builds a keyring from an age X25519 identity string
// ("AGE-SECRET-KEY-1..."). If no recipient strings ("age1...") are given, the
// identity's own recipient is used.
func FromX25519(identity string, recipients ...string) (*Keyring, error) {
	id, err := age.ParseX25519Identity(identity)
	if err != nil {
		return nil, err
	}
	rcpts := make([]age.Recipient, 0, len(recipients))
	for _, r := range recipients {
		rcpt, err := age.ParseX25519Recipient(r)
		if err != nil {
			return nil, err
		}
		rcpts = append(rcpts, rcpt)
	}
	if len(rcpts) == 0 {
		rcpts = append(rcpts, id.Recipient())
	}
	return New([]age.Identity{id}, rcpts), nil
}

// Generate creates a fresh random data key for id, wraps it to the recipients,
// stores it (active if it is the first key), and returns the wrapped blob so the
// caller can persist it for later AddWrapped.
func (t *Keyring) Generate(id uint32) ([]byte, error) {
	dataKey := make([]byte, dataKeySize)
	if _, err := io.ReadFull(rand.Reader, dataKey); err != nil {
		return nil, err
	}
	blob, err := t.wrap(dataKey)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.wrapped[id] = blob
	t.plain[id] = dataKey
	if !t.hasActive {
		t.active = id
		t.hasActive = true
	}
	t.mu.Unlock()
	return blob, nil
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

// Get implements cryptostore.Keyring, unwrapping (and caching) on demand.
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
	key, err := t.unwrap(blob)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.plain[id] = key
	t.mu.Unlock()
	return key, nil
}

func (t *Keyring) wrap(dataKey []byte) ([]byte, error) {
	if len(t.recipients) == 0 {
		return nil, ErrNoRecipients
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, t.recipients...)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(dataKey); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (t *Keyring) unwrap(blob []byte) ([]byte, error) {
	if len(t.identities) == 0 {
		return nil, ErrNoIdentities
	}
	r, err := age.Decrypt(bytes.NewReader(blob), t.identities...)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}
