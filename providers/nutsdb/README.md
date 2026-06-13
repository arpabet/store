# nutsdb store impl

nutsdb store interface impl.

Capabilities: `TTL | Atomic | Transaction | Ordered | Watch | BatchAtomic`

- Native transactions (a single writer at a time gives serialized
  read-modify-write), exposed via `BeginTransaction`/`EndTransaction`.
- Bucket model maps to the bolt-style `bucket:key` separator: a full key
  `"bucket:key"` is stored under nutsdb bucket `bucket`.
- Native timer-wheel TTL reclaims expired entries on disk without a sweeper, so
  this provider does not implement `Sweepable`.
- Watch is served from an in-process hub; events for writes inside an explicit
  transaction are published on commit.
