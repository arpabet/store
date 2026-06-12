/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package store

import "strings"

/**
Capability is a bitmask describing the guarantees a particular DataStore
implementation actually provides. Two stores are interchangeable for a given
feature only if both report the corresponding capability.

The conformance suite (go.arpabet.com/store/storetest) uses Features() to decide
which behavioral assertions apply to a provider, so a store must never advertise
a capability it does not honor.
*/

type Capability uint64

const (
	// TTLCapability means SetRaw/TouchRaw honor ttlSeconds and entries expire.
	TTLCapability Capability = 1 << iota

	// AtomicCapability means CompareAndSetRaw and IncrementRaw are atomic and
	// version-aware (GetRaw returns a meaningful, changing Version).
	AtomicCapability

	// TransactionCapability means the store implements TransactionalManager.
	TransactionCapability

	// EncryptedCapability means values are encrypted at rest.
	EncryptedCapability

	// OrderedCapability means EnumerateRaw returns keys in sorted (lexical) order.
	OrderedCapability

	// WatchCapability means WatchRaw delivers change notifications.
	WatchCapability
)

var capabilityNames = []struct {
	cap  Capability
	name string
}{
	{TTLCapability, "TTL"},
	{AtomicCapability, "Atomic"},
	{TransactionCapability, "Transaction"},
	{EncryptedCapability, "Encrypted"},
	{OrderedCapability, "Ordered"},
	{WatchCapability, "Watch"},
}

// Has reports whether all the bits in other are set in c.
func (c Capability) Has(other Capability) bool {
	return c&other == other
}

// String renders the capability set as a pipe-separated list, e.g. "TTL|Atomic|Ordered".
func (c Capability) String() string {
	if c == 0 {
		return "None"
	}
	var parts []string
	for _, e := range capabilityNames {
		if c.Has(e.cap) {
			parts = append(parts, e.name)
		}
	}
	return strings.Join(parts, "|")
}
