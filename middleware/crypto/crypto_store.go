/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

// Package cryptostore is a middleware that transparently encrypts values at rest
// for any store.ManagedDataStore. It wraps a delegate store, sealing values with
// AES-GCM before they reach the backend and opening them on read, so the
// "Encrypted" capability becomes available on engines that lack native
// encryption (bolt, bbolt, pebble, mem) — not just Badger.
//
// Each stored value carries the id of the key that sealed it:
//
//	[ 4 bytes key-id ][ 12 bytes nonce ][ AES-GCM ciphertext ]
//
// The key-id is authenticated as additional data, and read resolves it through
// the Keyring, so key rotation is online: rotate the active key and existing
// values keep decrypting under their original key while new writes use the new
// one. Keys are never encrypted (they must stay comparable for ordering/prefix
// scans); only values are. TTL and version metadata are handled by the
// underlying store and preserved.
//
// age, AWS KMS and GCP KMS integrations are provided as separate modules that
// implement Keyring, so their SDKs are not pulled in unless used.
package cryptostore

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"sync"

	"go.arpabet.com/store"
)

var ErrCiphertextTooShort = errors.New("cryptostore: ciphertext too short")

const keyIDHeaderLen = 4

type cryptoStore struct {
	delegate store.ManagedDataStore
	keyring  Keyring

	mu    sync.RWMutex
	aeads map[uint32]cipher.AEAD // cache of id -> AEAD
}

// New wraps delegate, encrypting all values with a single key (id 1). The key
// length selects AES-128/192/256 (16/24/32 bytes). For rotation, use
// NewWithKeyring.
func New(delegate store.ManagedDataStore, key []byte) (store.ManagedDataStore, error) {
	return NewWithKeyring(delegate, NewStaticKeyring().Add(1, key))
}

// NewWithKeyring wraps delegate, sealing new values with the keyring's active
// key and decrypting existing values by their stored key id.
func NewWithKeyring(delegate store.ManagedDataStore, keyring Keyring) (store.ManagedDataStore, error) {
	t := &cryptoStore{delegate: delegate, keyring: keyring, aeads: make(map[uint32]cipher.AEAD)}
	// validate the active key up front (correct length, present)
	id, key, err := keyring.Active()
	if err != nil {
		return nil, err
	}
	if _, err := t.aeadFor(id, key); err != nil {
		return nil, err
	}
	return t, nil
}

// aeadFor returns (and caches) the AEAD for a key id. If key is nil it is
// resolved from the keyring (read path); a non-nil key avoids a lookup (write path).
func (t *cryptoStore) aeadFor(id uint32, key []byte) (cipher.AEAD, error) {
	t.mu.RLock()
	a, ok := t.aeads[id]
	t.mu.RUnlock()
	if ok {
		return a, nil
	}
	if key == nil {
		var err error
		if key, err = t.keyring.Get(id); err != nil {
			return nil, err
		}
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	a, err = cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.aeads[id] = a
	t.mu.Unlock()
	return a, nil
}

func (t *cryptoStore) seal(plaintext []byte) ([]byte, error) {
	id, key, err := t.keyring.Active()
	if err != nil {
		return nil, err
	}
	aead, err := t.aeadFor(id, key)
	if err != nil {
		return nil, err
	}
	header := make([]byte, keyIDHeaderLen)
	binary.BigEndian.PutUint32(header, id)
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	out := make([]byte, 0, keyIDHeaderLen+len(nonce)+len(plaintext)+aead.Overhead())
	out = append(out, header...)
	out = append(out, nonce...)
	// header is authenticated as additional data so the key id cannot be swapped
	return aead.Seal(out, nonce, plaintext, header), nil
}

func (t *cryptoStore) open(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < keyIDHeaderLen {
		return nil, ErrCiphertextTooShort
	}
	header := ciphertext[:keyIDHeaderLen]
	id := binary.BigEndian.Uint32(header)
	aead, err := t.aeadFor(id, nil)
	if err != nil {
		return nil, err
	}
	ns := aead.NonceSize()
	if len(ciphertext) < keyIDHeaderLen+ns {
		return nil, ErrCiphertextTooShort
	}
	nonce := ciphertext[keyIDHeaderLen : keyIDHeaderLen+ns]
	ct := ciphertext[keyIDHeaderLen+ns:]
	return aead.Open(nil, nonce, ct, header)
}

// --- glue / management (delegate, but report the added capability) ---

func (t *cryptoStore) BeanName() string { return t.delegate.BeanName() }
func (t *cryptoStore) Destroy() error   { return t.delegate.Destroy() }

func (t *cryptoStore) Features() store.Capability {
	return t.delegate.Features() | store.EncryptedCapability
}

func (t *cryptoStore) Compact(discardRatio float64) error { return t.delegate.Compact(discardRatio) }
func (t *cryptoStore) Backup(w io.Writer, since uint64) (uint64, error) {
	return t.delegate.Backup(w, since)
}
func (t *cryptoStore) Restore(r io.Reader) error      { return t.delegate.Restore(r) }
func (t *cryptoStore) DropAll() error                 { return t.delegate.DropAll() }
func (t *cryptoStore) DropWithPrefix(p []byte) error  { return t.delegate.DropWithPrefix(p) }
func (t *cryptoStore) Instance() interface{}          { return t.delegate.Instance() }

// --- sugar (must bind operations to this wrapper, not the delegate) ---

func (t *cryptoStore) Get(ctx context.Context) *store.GetOperation {
	return &store.GetOperation{DataStore: t, Context: ctx}
}
func (t *cryptoStore) Set(ctx context.Context) *store.SetOperation {
	return &store.SetOperation{DataStore: t, Context: ctx}
}
func (t *cryptoStore) Batch(ctx context.Context) *store.BatchOperation {
	return &store.BatchOperation{DataStore: t, Context: ctx}
}
func (t *cryptoStore) CompareAndSet(ctx context.Context) *store.CompareAndSetOperation {
	return &store.CompareAndSetOperation{DataStore: t, Context: ctx}
}
func (t *cryptoStore) Increment(ctx context.Context) *store.IncrementOperation {
	return &store.IncrementOperation{DataStore: t, Context: ctx, Initial: 0, Delta: 1}
}
func (t *cryptoStore) Touch(ctx context.Context) *store.TouchOperation {
	return &store.TouchOperation{DataStore: t, Context: ctx}
}
func (t *cryptoStore) Remove(ctx context.Context) *store.RemoveOperation {
	return &store.RemoveOperation{DataStore: t, Context: ctx}
}
func (t *cryptoStore) Enumerate(ctx context.Context) *store.EnumerateOperation {
	return &store.EnumerateOperation{DataStore: t, Context: ctx}
}
func (t *cryptoStore) Watch(ctx context.Context) *store.WatchOperation {
	return &store.WatchOperation{DataStore: t, Context: ctx}
}

// --- raw operations (encrypt/decrypt around the delegate) ---

func (t *cryptoStore) GetRaw(ctx context.Context, key []byte, ttlPtr *int, versionPtr *int64, required bool) ([]byte, error) {
	enc, err := t.delegate.GetRaw(ctx, key, ttlPtr, versionPtr, required)
	if err != nil || enc == nil {
		return nil, err
	}
	return t.open(enc)
}

func (t *cryptoStore) SetRaw(ctx context.Context, key, value []byte, ttlSeconds int) error {
	enc, err := t.seal(value)
	if err != nil {
		return err
	}
	return t.delegate.SetRaw(ctx, key, enc, ttlSeconds)
}

func (t *cryptoStore) SetBatchRaw(ctx context.Context, entries []store.RawEntry) error {
	sealed := make([]store.RawEntry, len(entries))
	for i := range entries {
		enc, err := t.seal(entries[i].Value)
		if err != nil {
			return err
		}
		sealed[i] = store.RawEntry{Key: entries[i].Key, Value: enc, Ttl: entries[i].Ttl}
	}
	return t.delegate.SetBatchRaw(ctx, sealed)
}

func (t *cryptoStore) CompareAndSetRaw(ctx context.Context, key, value []byte, ttlSeconds int, version int64) (bool, error) {
	enc, err := t.seal(value)
	if err != nil {
		return false, err
	}
	return t.delegate.CompareAndSetRaw(ctx, key, enc, ttlSeconds, version)
}

// IncrementRaw cannot delegate to the backend (it would do arithmetic on
// ciphertext), so it performs the read-modify-write at this layer. When the
// delegate is atomic it uses an optimistic CAS retry loop; otherwise it falls
// back to a (non-atomic) get/set.
func (t *cryptoStore) IncrementRaw(ctx context.Context, key []byte, initial, delta int64, ttlSeconds int) (int64, error) {
	atomic := t.delegate.Features().Has(store.AtomicCapability)
	const maxRetries = 100
	for attempt := 0; ; attempt++ {
		var version int64
		cur, err := t.GetRaw(ctx, key, nil, &version, false)
		if err != nil {
			return 0, err
		}
		prev := initial
		if len(cur) >= 8 {
			prev = int64(beUint64(cur))
		}
		next := prev + delta
		buf := bePutUint64(uint64(next))

		if atomic {
			ok, err := t.CompareAndSetRaw(ctx, key, buf, ttlSeconds, version)
			if err != nil {
				return 0, err
			}
			if ok {
				return prev, nil
			}
			if attempt >= maxRetries {
				return 0, store.ErrConcurrentTxn
			}
			continue
		}

		if err := t.SetRaw(ctx, key, buf, ttlSeconds); err != nil {
			return 0, err
		}
		return prev, nil
	}
}

func (t *cryptoStore) TouchRaw(ctx context.Context, key []byte, ttlSeconds int) error {
	return t.delegate.TouchRaw(ctx, key, ttlSeconds)
}

func (t *cryptoStore) RemoveRaw(ctx context.Context, key []byte) error {
	return t.delegate.RemoveRaw(ctx, key)
}

func (t *cryptoStore) EnumerateRaw(ctx context.Context, prefix, seek []byte, batchSize int, onlyKeys bool, reverse bool, cb func(*store.RawEntry) bool) error {
	var cbErr error
	err := t.delegate.EnumerateRaw(ctx, prefix, seek, batchSize, onlyKeys, reverse, func(e *store.RawEntry) bool {
		if !onlyKeys && len(e.Value) > 0 {
			plain, derr := t.open(e.Value)
			if derr != nil {
				cbErr = derr
				return false
			}
			e.Value = plain
		}
		return cb(e)
	})
	if err == nil {
		err = cbErr
	}
	return err
}

func (t *cryptoStore) WatchRaw(ctx context.Context, prefix []byte, cb func(*store.WatchEvent) bool) error {
	var cbErr error
	err := t.delegate.WatchRaw(ctx, prefix, func(e *store.WatchEvent) bool {
		if e.Type == store.WatchSet && len(e.Value) > 0 {
			plain, derr := t.open(e.Value)
			if derr != nil {
				cbErr = derr
				return false
			}
			e.Value = plain
		}
		return cb(e)
	})
	if err == nil {
		err = cbErr
	}
	return err
}

func beUint64(b []byte) uint64 {
	return uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
		uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7])
}

func bePutUint64(v uint64) []byte {
	return []byte{
		byte(v >> 56), byte(v >> 48), byte(v >> 40), byte(v >> 32),
		byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v),
	}
}
