# bolt store impl

> **⚠️ Deprecated.** This provider wraps [`github.com/boltdb/bolt`](https://github.com/boltdb/bolt),
> which was **archived by its maintainers in 2018** and receives no fixes. It is
> frozen here (bugfixes only) for existing users.
>
> **Use [`providers/bbolt`](../bbolt) instead** — it wraps `go.etcd.io/bbolt`, the
> actively maintained drop-in fork, and exposes the identical `store` API.
> Migration is a one-line import change:
>
> ```go
> // before
> import boltstore "go.arpabet.com/store/providers/bolt"
> // after
> import boltstore "go.arpabet.com/store/providers/bbolt"
> ```

bolt store implementation
