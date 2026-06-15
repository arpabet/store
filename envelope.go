/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package store

import (
	"encoding/binary"
	"time"
)

/**
Value envelope used by backends that lack native TTL and versioning
(bbolt, bolt, pebble, mem). The envelope prepends a fixed header to the
user value so those engines can offer the same TTL/version/CAS semantics
as Badger without changing their on-disk key layout:

	[ 1 byte magic ][ 8 bytes version ][ 8 bytes expiresAt unix ][ value... ]

version is a per-key monotonic counter (1 on first write, +1 each write).
expiresAt is a unix timestamp in seconds, 0 means no expiry.

Backends that store envelopes always read them back, so DecodeEnvelope only
returns ok=false for legacy/foreign values, which are surfaced verbatim with
zero metadata for backward compatibility.
*/

const envelopeMagic byte = 0xE1

// EnvelopeHeaderLen is the fixed size of the envelope header in bytes.
const EnvelopeHeaderLen = 1 + 8 + 8

// EncodeEnvelope wraps value with version and expiry metadata.
func EncodeEnvelope(version, expiresAtUnix int64, value []byte) []byte {
	buf := make([]byte, EnvelopeHeaderLen+len(value))
	buf[0] = envelopeMagic
	binary.BigEndian.PutUint64(buf[1:9], uint64(version))
	binary.BigEndian.PutUint64(buf[9:17], uint64(expiresAtUnix))
	copy(buf[EnvelopeHeaderLen:], value)
	return buf
}

// DecodeEnvelope unwraps an envelope. If raw is not an envelope (legacy value),
// it returns ok=false with the original bytes as the value and zero metadata.
// The returned value aliases raw; copy it if it must outlive raw's storage.
func DecodeEnvelope(raw []byte) (version, expiresAtUnix int64, value []byte, ok bool) {
	if len(raw) < EnvelopeHeaderLen || raw[0] != envelopeMagic {
		return 0, 0, raw, false
	}
	version = int64(binary.BigEndian.Uint64(raw[1:9]))
	expiresAtUnix = int64(binary.BigEndian.Uint64(raw[9:17]))
	value = raw[EnvelopeHeaderLen:]
	return version, expiresAtUnix, value, true
}

// ExpiryFromTtl converts a relative ttl (seconds) into an absolute unix
// timestamp. ttlSeconds <= 0 (NoTTL) yields 0, meaning no expiry.
func ExpiryFromTtl(ttlSeconds int) int64 {
	if ttlSeconds <= NoTTL {
		return 0
	}
	return time.Now().Unix() + int64(ttlSeconds)
}

// TtlFromExpiry converts an absolute expiry timestamp into the remaining ttl in
// seconds. It returns NoTTL (0) when there is no expiry; a value with an expiry
// that has essentially no time left reports -1 to stay distinguishable from NoTTL.
func TtlFromExpiry(expiresAtUnix int64) int {
	if expiresAtUnix == 0 {
		return NoTTL
	}
	v := int(expiresAtUnix - time.Now().Unix())
	if v == 0 {
		v = -1
	}
	return v
}

// IsExpired reports whether an entry with the given absolute expiry is expired now.
func IsExpired(expiresAtUnix int64) bool {
	return expiresAtUnix != 0 && time.Now().Unix() >= expiresAtUnix
}
