# store

Transactional, capability-aware Data Store interface over several embedded
key-value engines. One uniform API (`Get/Set/Increment/CompareAndSet/Touch/Remove/Enumerate/Watch`,
context-propagated transactions, protobuf codec) lets you select and migrate
between engines without rewriting application code.

This is a multi-module repository (one `go.mod` per provider/middleware), so an
application only pulls the engine it actually imports. See `go.work` for the
local development workspace.

## Layout

```
store/                      module go.arpabet.com/store              (the interface)
  storetest/                module .../storetest                     (conformance suite)
  providers/badger/         module .../providers/badger              (BadgerDB v4)
  providers/pebble/         module .../providers/pebble              (PebbleDB v2)
  providers/bbolt/          module .../providers/bbolt               (etcd bbolt)
  providers/bolt/           module .../providers/bolt                (boltdb, legacy)
  providers/mem/            module .../providers/mem                 (in-memory cache)
  middleware/crypto/        module .../middleware/crypto             (AES-GCM at rest)
  middleware/otel/          module .../middleware/otel               (OpenTelemetry tracing)
```

## Capabilities

Every store reports what it actually guarantees via `Features() Capability`, and
the shared conformance suite (`storetest`) asserts a behavior for a provider only
if it advertises the matching capability. Providers are interchangeable for a
given feature when both report it.

Provider | Backend | Ordered | TTL | Atomic/CAS | Watch | Transactional | Encrypted | Usage
--- | --- | --- | --- | --- | --- | --- | --- | ---
providers/badger | BadgerDB v4 | Y | Y | Y | Y | Y | Y (native) | User / PII data
providers/pebble | PebbleDB v2 | Y | Y | Y | Y | N | via crypto | App data
providers/bbolt  | etcd bbolt  | Y | Y | Y | Y | N | via crypto | Config
providers/bolt   | boltdb (legacy) | Y | Y | Y | Y | N | via crypto | Config (legacy)
providers/mem    | ttlcache v3 | N | Y | Y | Y | N | via crypto | Hot data

TTL, versioning and compare-and-set on the non-Badger engines are provided by a
shared value envelope (`version | expiresAt | value`) with lazy expiry; watch on
those engines is served by an in-process fan-out hub. Badger uses its native
TTL, MVCC versions, transactions and `Subscribe`.

## Middleware

Middleware are decorators that wrap any `store.ManagedDataStore` and are
themselves stores, so they compose and remain interchangeable:

- **crypto** — encrypts values at rest with AES-GCM (keys stay plaintext for
  ordering/prefix scans). Adds the `Encrypted` capability to any backend, so
  "encrypted config in bbolt" or "encrypted hot data in mem" is a one-liner.
  Each value stores the id of the key that sealed it, so a `Keyring` enables
  **online key rotation**: old values keep decrypting under their old key while
  new writes use the active key. age / AWS KMS / GCP KMS integrations are
  separate modules implementing `Keyring`, so their SDKs aren't pulled in unless used.
- **otel** — wraps every operation in an OpenTelemetry span (bean name, op, key;
  values are never recorded, safe for PII).

```go
base, _ := bboltstore.New("config", "config.db", 0666)
enc, _  := cryptostore.New(base.Interface(), key) // values now encrypted at rest
s       := otelstore.New(enc)                      // + tracing

// rotation: add a new key, make it active; old values still readable
kr := cryptostore.NewStaticKeyring().Add(1, oldKey)
enc, _ = cryptostore.NewWithKeyring(base.Interface(), kr)
kr.Add(2, newKey); kr.SetActive(2)               // new writes use key 2
```

## Releasing

All modules are versioned together with `release.sh`. See [RELEASING.md](RELEASING.md).

```bash
./release.sh --dry-run v1.3.0   # preview
./release.sh v1.3.0             # tag + push every module
```
