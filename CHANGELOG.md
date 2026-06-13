# Changelog

## v1.1.0

This release expands `store` from a bare interface (`v1.0.x`) into a complete, multi-module ecosystem: seven storage providers, three middleware decorators, a generics-based typed layer, a shared conformance + benchmark suite, and a capability model that keeps every backend interchangeable.

The repository is now multi-module (one `go.mod` per provider/middleware), so an application only pulls in the engine it actually imports. See `go.work` for the local development workspace.

### Highlights

**Capability-aware interface.** Every store reports what it actually guarantees via `Features() Capability` (`Ordered`, `TTL`, `Atomic/CAS`, `Watch`, `Transactional`, `BatchAtomic`, `Encrypted`). The shared `storetest` conformance suite asserts a behavior for a provider only if it advertises the matching capability, so providers are provably interchangeable for any feature both report.

**New methods on the interface.** `Watch` (in-process, best-effort change notifications), `Batch` / `SetBatchRaw` (batch writes on every store; `BatchAtomic` marks the all-or-nothing ones), and richer `Enumerate` with `Range(from, to)` over the half-open `[from, to)` interval plus `.After(token).Limit(n).DoPage(...)` opaque-token pagination.

### Providers

| Provider | Backend | Notes |
| --- | --- | --- |
| `providers/badger` | BadgerDB v4 | Native TTL, MVCC versions, transactions, `Subscribe`, native encryption |
| `providers/pebble` | PebbleDB v2 | Ordered, atomic batches |
| `providers/bbolt` | etcd bbolt | Config-oriented |
| `providers/bolt` | boltdb (legacy) | Deprecated, kept for migration |
| `providers/mem` | ttlcache v3 | Hot data; sorts on enumerate to satisfy `Ordered` |
| `providers/rosedb` | rosedb (Bitcask) | Native disk TTL (no sweeper), native ordered iteration, atomic batches |
| `providers/nutsdb` | nutsdb | Native transactions + timer-wheel TTL; bucket model maps to `bucket:key` |

For the non-native engines, TTL, versioning, and compare-and-set are provided by a shared value envelope (`version | expiresAt | value`) with lazy expiry, and watch is served by an in-process fan-out hub. A background `store.StartSweeper` reclaims expired entries on the disk backends.

### Middleware

Middleware are decorators that wrap any `store.ManagedDataStore` and are themselves stores, so they compose and stay interchangeable.

- **crypto** — AES-GCM encryption at rest (keys stay plaintext for ordering/prefix scans). Adds the `Encrypted` capability to any backend. Each value records the id of the key that sealed it, so a `Keyring` enables **online key rotation** — old values keep decrypting under their old key while new writes use the active key. Key sources are separate modules so their SDKs aren't pulled in unless used: `crypto/age` (age-wrapped data keys), `crypto/kms/aws` (AWS KMS), and `crypto/kms/gcp` (Google Cloud KMS).
- **compress** — transparent per-value compression (zstd or snappy) with a codec marker; incompressible/tiny values are stored uncompressed so a value is never inflated. Compose *outside* crypto (`compress(crypto(base))`) to compress plaintext before encryption.
- **otel** — OpenTelemetry observability: a span per operation plus metrics (`store.operations`, `store.errors`, `store.operation.duration`). Keys/ops are recorded; values never are.

### Typed access (generics)

A codec-driven typed layer over the raw byte API (pure sugar, no provider requirements), with built-in `store.JSON[T]()`, `store.MsgPack[T]()`, and `store.Proto[*pb.T]()`:

```go
users := store.Of[*pb.User](ds, store.Proto[*pb.User]())
users.Put(ctx, []byte("u:1"), &pb.User{Name: "Ann"}, store.NoTTL)
u, found, err := users.Get(ctx, []byte("u:1"))
```

### Tooling

- `storetest` — shared conformance suite and benchmark harness.
- `benchmarks` — cross-engine benchmark runner, plus CI workflows (`build.yml`, `benchmark.yml`).
- `RELEASING.md` + `release.sh` — documented, scripted multi-module release process.

### Watch semantics

Watch is **in-process** (only mutations through this process's store handle are observed) and **best-effort** (bounded per-watcher buffers drop events under load — treat an event as "re-read this key"). Writes made directly on the engine returned by `Instance()` bypass the store layer and are **not** delivered to watchers.
