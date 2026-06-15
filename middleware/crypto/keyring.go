/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package cryptostore

import (
	"errors"
	"sync"
)

var (
	ErrNoActiveKey  = errors.New("cryptostore: keyring has no active key")
	ErrUnknownKeyID = errors.New("cryptostore: unknown key id")
)

/**
Keyring supplies the symmetric keys cryptostore uses. Each value is stored with
the id of the key that sealed it, so a write always uses the Active key while a
read resolves the historical key by id via Get — this is what makes key rotation
online: rotate the active key and old values keep decrypting under their old key.

Implementations can be backed by anything that yields a raw AES key (16/24/32
bytes): a static set, an age-wrapped data key, or a KMS data key unwrapped on
demand. Those adapters live in separate modules so their SDKs are not pulled in
by everyone. Keyring methods must be safe for concurrent use.
*/

type Keyring interface {
	// Active returns the id and key new writes should be sealed with.
	Active() (id uint32, key []byte, err error)
	// Get returns the key for the given id (used to decrypt existing values).
	Get(id uint32) (key []byte, err error)
}

// StaticKeyring is an in-memory Keyring. Add keys, optionally SetActive, and
// rotate at runtime by adding a new key and making it active.
type StaticKeyring struct {
	mu        sync.RWMutex
	keys      map[uint32][]byte
	active    uint32
	hasActive bool
}

func NewStaticKeyring() *StaticKeyring {
	return &StaticKeyring{keys: make(map[uint32][]byte)}
}

// Add registers a key under id. The first key added becomes active. Chainable.
func (t *StaticKeyring) Add(id uint32, key []byte) *StaticKeyring {
	dup := make([]byte, len(key))
	copy(dup, key)
	t.mu.Lock()
	t.keys[id] = dup
	if !t.hasActive {
		t.active = id
		t.hasActive = true
	}
	t.mu.Unlock()
	return t
}

// SetActive selects which registered key seals new writes (the rotation point).
func (t *StaticKeyring) SetActive(id uint32) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.keys[id]; !ok {
		return ErrUnknownKeyID
	}
	t.active = id
	t.hasActive = true
	return nil
}

func (t *StaticKeyring) Active() (uint32, []byte, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if !t.hasActive {
		return 0, nil, ErrNoActiveKey
	}
	return t.active, t.keys[t.active], nil
}

func (t *StaticKeyring) Get(id uint32) ([]byte, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	key, ok := t.keys[id]
	if !ok {
		return nil, ErrUnknownKeyID
	}
	return key, nil
}
