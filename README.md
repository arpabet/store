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
  storetest/                module .../storetest                     (conformance + benchmark suite)
  benchmarks/               module .../benchmarks                    (cross-engine benchmark runner)
  providers/badger/         module .../providers/badger              (BadgerDB v4)
  providers/pebble/         module .../providers/pebble              (PebbleDB v2)
  providers/bbolt/          module .../providers/bbolt               (etcd bbolt)
  providers/bolt/           module .../providers/bolt                (boltdb, legacy)
  providers/mem/            module .../providers/mem                 (in-memory cache)
  providers/rosedb/         module .../providers/rosedb              (Bitcask, native TTL)
  providers/nutsdb/         module .../providers/nutsdb              (nutsdb, native txns + TTL)
  middleware/crypto/        module .../middleware/crypto             (AES-GCM at rest)
  middleware/otel/          module .../middleware/otel               (OpenTelemetry tracing)
```

## Capabilities

Every store reports what it actually guarantees via `Features() Capability`, and
the shared conformance suite (`storetest`) asserts a behavior for a provider only
if it advertises the matching capability. Providers are interchangeable for a
given feature when both report it.

Provider | Backend | Ordered | TTL | Atomic/CAS | Watch | Transactional | BatchAtomic | Encrypted | Usage
--- | --- | --- | --- | --- | --- | --- | --- | --- | ---
providers/badger | BadgerDB v4 | Y | Y | Y | Y | Y | N | Y (native) | User / PII data
providers/pebble | PebbleDB v2 | Y | Y | Y | Y | N | Y | via crypto | App data
providers/bbolt  | etcd bbolt  | Y | Y | Y | Y | N | Y | via crypto | Config
providers/bolt   | boltdb (legacy) | Y | Y | Y | Y | N | Y | via crypto | Config (legacy)
providers/mem    | ttlcache v3 | Y | Y | Y | Y | N | N | via crypto | Hot data
providers/rosedb | rosedb (Bitcask) | Y | Y | Y | Y | N | Y | via crypto | App data
providers/nutsdb | nutsdb | Y | Y | Y | Y | Y | Y | via crypto | App data

Batch writes (`s.Batch(ctx).Put(...).Do()` / `SetBatchRaw`) are available on every
store; `BatchAtomic` marks the ones where a batch is all-or-nothing (single
transaction). Badger uses `WriteBatch` (chunked, not atomic) and mem applies under
a lock (not isolated from readers), so neither advertises `BatchAtomic`.

Range scans and pagination need ordering: `Enumerate(ctx).Range(from, to)` over the
half-open `[from, to)` interval, and `.After(token).Limit(n).DoPage(...)` which
returns an opaque continuation token (the last key) for the next page. These
return `ErrNotSupported` on a store without `Ordered`; mem sorts on enumerate so it
qualifies.

TTL, versioning and compare-and-set on the non-Badger engines are provided by a
shared value envelope (`version | expiresAt | value`) with lazy expiry; watch on
those engines is served by an in-process fan-out hub. Badger uses its native
TTL, MVCC versions, transactions and `Subscribe`. rosedb uses native TTL (disk
expiry — so it needs no sweeper) and native ordered iteration and atomic batches,
with the envelope carrying the version and watch served by the hub.

nutsdb adds native transactions (exposed via `BeginTransaction`/`EndTransaction`,
like Badger; a single writer at a time gives serialized read-modify-write) and
native timer-wheel TTL (disk expiry — so it needs no sweeper). Its bucket model
maps to the bolt-style `bucket:key` separator: a full key `"bucket:key"` is
stored under nutsdb bucket `bucket`, with each bucket a BTree for ordered scans.
The envelope carries the version; watch is served by the hub, and events for
writes inside an explicit transaction are published on commit.

**Watch semantics.** Watch is **in-process** (mutations through this process's
handle only; a different process writing the same file is not observed) and
**best-effort** (bounded per-watcher buffers drop events under load — treat an
event as "re-read this key"). TTL expiry surfaces as a `WatchDelete` only when the
entry is reclaimed: automatically on mem, and via a running sweeper
(`store.StartSweeper`) on the disk backends. Writes made directly on the engine
returned by `Instance()` bypass the store layer and are **not** delivered to watchers.

## Typed access (generics)

A codec-driven typed layer sits on top of the raw byte API (pure sugar, no
provider requirements). Built-in `Codec[T]`: `store.JSON[T]()`, `store.MsgPack[T]()`,
and `store.Proto[*pb.T]()`.

```go
users := store.Of[*pb.User](ds, store.Proto[*pb.User]())
users.Put(ctx, []byte("u:1"), &pb.User{Name: "Ann"}, store.NoTTL)
u, found, err := users.Get(ctx, []byte("u:1"))

// or as free functions
err = store.Put(ctx, ds, []byte("c:1"), cfg, store.JSON[Config](), store.NoTTL)
cfg, found, err := store.Get(ctx, ds, []byte("c:1"), store.JSON[Config]())
```

## Middleware

Middleware are decorators that wrap any `store.ManagedDataStore` and are
themselves stores, so they compose and remain interchangeable:

- **crypto** — encrypts values at rest with AES-GCM (keys stay plaintext for
  ordering/prefix scans). Adds the `Encrypted` capability to any backend, so
  "encrypted config in bbolt" or "encrypted hot data in mem" is a one-liner.
  Each value stores the id of the key that sealed it, so a `Keyring` enables
  **online key rotation**: old values keep decrypting under their old key while
  new writes use the active key. Key sources are separate modules implementing
  `Keyring`, so their SDKs aren't pulled in unless used:
  [`middleware/crypto/age`](middleware/crypto/age) (age-wrapped data keys),
  [`middleware/crypto/kms/aws`](middleware/crypto/kms/aws) (AWS KMS) and
  [`middleware/crypto/kms/gcp`](middleware/crypto/kms/gcp) (Google Cloud KMS).
- **compress** — transparently compresses values (zstd or snappy) with a
  per-value codec marker; incompressible/tiny values are stored uncompressed so
  a value is never inflated. Compose it *outside* crypto
  (`compress(crypto(base))`) so plaintext is compressed before it is encrypted.
- **otel** — OpenTelemetry observability: a span per operation **plus metrics**
  (operation counter `store.operations`, error counter `store.errors`, latency
  histogram `store.operation.duration`). Bean/op are recorded; values never are.
  Prometheus users scrape via the OTel Prometheus bridge.

```go
base, _ := bboltstore.New("config", "config.db", 0600)
enc, _  := cryptostore.New(base.Interface(), key) // values now encrypted at rest
s       := otelstore.New(enc)                      // + tracing

// rotation: add a new key, make it active; old values still readable
kr := cryptostore.NewStaticKeyring().Add(1, oldKey)
enc, _ = cryptostore.NewWithKeyring(base.Interface(), kr)
kr.Add(2, newKey); kr.SetActive(2)               // new writes use key 2
```

## Testing & benchmarks

`storetest` is the shared, capability-gated suite every provider must pass — a
behavior is asserted only if the provider advertises the matching capability:

- `storetest.RunConformance(t, factory)` — correctness (get/set/remove, TTL and
  expiry, batch + atomic batch, CAS/version, ordered enumeration, range (forward
  and reverse), pagination, sweep, watch).
- `storetest.RunRotation(t, factory)` — online key rotation for encrypted stores
  (pre-rotation values still decrypt; new writes use the active key).
- `storetest.RunBenchmarks(b, factory)` — the shared benchmark suite (Set, Get,
  Batch, Increment, CompareAndSet, Enumerate, Range, Sweep), gated the same way.

The `benchmarks` module wires `RunBenchmarks` to every engine plus a crypto key
rotation benchmark, so they can be compared head-to-head:

```bash
go test -run '^$' -bench . -benchmem ./benchmarks/...      # all engines
go test -run '^$' -bench BenchmarkNutsdb/Set ./benchmarks/...
```

CI runs the conformance suite per module on every push/PR (`.github/workflows/build.yml`);
benchmarks are heavy and noisy, so they run on demand and weekly via a separate
workflow (`.github/workflows/benchmark.yml`) that publishes results as an artifact.

## Releasing

All modules are versioned together with `release.sh`. See [RELEASING.md](RELEASING.md).

```bash
./release.sh --dry-run v1.3.0   # preview
./release.sh v1.3.0             # tag + push every module
```
