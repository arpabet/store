# Releasing

This is a multi-module repository: the `store` interface and every provider /
middleware is its own Go module with its own version tag. `release.sh` cuts a
coordinated release across all of them.

The guiding rule: **one shared version for everyone.** An interface change ripples
into all modules, so they move together. A module that carries an extra change on
top of the shared version gets a higher **patch** via a per-module override.

## TL;DR

```bash
# inspect the plan, change nothing
./release.sh --dry-run v1.3.0

# cut and push the release
./release.sh v1.3.0
```

## Cutting a coordinated minor (Phase 2 → `v1.2.0`)

Phase 2 only **adds** to the `store` interface — the capability/watch/sweeper/
transaction surface, the `providers/nutsdb` engine, and the `storetest`
`RunRotation` / `RunBenchmarks` helpers — without changing or removing any
existing signature. Additive, backward-compatible changes are a **minor** bump
under semver (`MINOR`+1, `PATCH`→0), and since an interface change ripples into
every module, they all move together to that one minor. **No per-module
overrides are needed**: nothing carries a change beyond the shared version, so
this is the plain `./release.sh v1.2.0` case.

`release.sh` discovers every module under the repo automatically
(`find . -name go.mod`), so the modules added in Phase 2 — `providers/nutsdb` and
the internal `benchmarks` runner — are tagged at `v1.2.0` alongside everyone
else; you never list them. (`benchmarks` is a workspace-only leaf no one imports;
it gets a tag for uniformity, which is harmless.)

Pick the next unused minor — `v1.2.0` here.

1. **Verify green first** — the script does not run tests:

   ```bash
   for d in . storetest benchmarks providers/* middleware/*; do (cd "$d" && go test ./...); done
   ```

2. **Preview the plan** (changes nothing):

   ```bash
   ./release.sh --dry-run v1.2.0
   ```

   Expect the root at `v1.2.0` and every submodule — `storetest/v1.2.0`,
   `providers/nutsdb/v1.2.0`, `benchmarks/v1.2.0`, `middleware/*/v1.2.0`, … —
   each pinned to the interface at `v1.2.0`, with **no** override lines.

3. **Cut and push** from a clean `main`:

   ```bash
   ./release.sh v1.2.0
   ```

   This pins `require go.arpabet.com/store@v1.2.0` in every module's `go.mod`,
   commits `release v1.2.0: pin go.arpabet.com/store@v1.2.0`, tags the root and
   each submodule at that commit, and pushes the branch + all tags. Use
   `--no-push` to stage the commit and tags locally and inspect them first.

4. **(Optional)** refresh each module's standalone `go.sum` once the
   `store@v1.2.0` tag is fetchable — see
   [After the release](#after-the-release-standalone-buildable-modules-optional).

Because the bump is purely additive, consumers on the previous release upgrade
with a plain `go get go.arpabet.com/store/...@v1.2.0` and nothing breaks. A later
fix to a single engine rides on top as a patch override (e.g.
`./release.sh v1.2.0 providers/nutsdb=v1.2.1`), as described below.

## Prerequisites

- Run from the repository root (where `.git`, `go.work` and the root `go.mod` live).
- A clean working tree (`git status` empty) — the script refuses otherwise.
- On `main`, up to date with `origin` (the script warns if you are not on `main`).
- `git` push access to `origin` (https://github.com/arpabet/store) and Go ≥ 1.25.
- Green CI / local tests for everything you are about to tag (the script does **not**
  run the test suite — verify first):

  ```bash
  for d in . storetest benchmarks providers/* middleware/*; do (cd "$d" && go test ./...); done
  ```

## Version rules

Tags must be valid 3-component semver: `vMAJOR.MINOR.PATCH` (optionally
`-prerelease`, e.g. `v1.3.0-rc1`).

**Four-number versions like `v1.2.3.4` are not valid Go module versions** and are
rejected by the `go` tool and the module proxy. To express "the shared version
plus an extra change in one module", bump that module's **patch** with an override
(see below). The script enforces this and will stop you.

Tag naming follows the multi-module convention automatically:

| Module | Tag |
| --- | --- |
| root (`go.arpabet.com/store`) | `v1.3.0` |
| `storetest` | `storetest/v1.3.0` |
| `providers/badger` | `providers/badger/v1.3.0` |
| `middleware/crypto` | `middleware/crypto/v1.3.0` |
| … | `<subpath>/v1.3.0` |

## What the script does

For `./release.sh v1.3.0`:

1. Validates the version(s) and refuses to clobber tags that already exist.
2. Bumps `require go.arpabet.com/store` to `v1.3.0` in every provider/middleware
   `go.mod` (so released modules reference the new interface, not the old one)
   and commits the change: `release v1.3.0: pin go.arpabet.com/store@v1.3.0`.
3. Creates annotated tags for the root module and every submodule at that commit.
4. Pushes the branch and all tags to `origin`.

Flags:

- `--dry-run` — print the plan (require bumps + tags) and exit without changing anything.
- `--no-push` — make the commit and tags locally but do not push (inspect, then push by hand).

## Releasing one module ahead of the rest

When only, say, Badger has an extra fix after a shared release, give it a higher
patch while everyone else stays put:

```bash
./release.sh v1.3.0 providers/badger=v1.3.1
```

This tags `providers/badger/v1.3.1` and everything else `v1.3.0`. The override only
changes Badger's **own tag** — Badger still requires the interface at `store@v1.3.0`.

You can override more than one module:

```bash
./release.sh v1.3.0 providers/badger=v1.3.1 middleware/crypto=v1.3.1
```

## After the release: standalone-buildable modules (optional)

Inside this repo, `go.work` makes every module build against the local interface,
so the freshly bumped `require go.arpabet.com/store@v1.3.0` resolves even before the
tag is fetchable. CI keeps working.

A module's own `go.sum` will **not** contain the `store@v1.3.0` checksum until the
store tag is published and fetched. This does not affect consumers (they compute
their own `go.sum` on `go get`) — it only matters if you want to `go build` a single
module **outside** the workspace. To make a module standalone-buildable, after the
tags are pushed:

```bash
cd providers/badger
GOWORK=off go get go.arpabet.com/store@v1.3.0
GOWORK=off go mod tidy
git add go.mod go.sum && git commit -m "providers/badger: refresh go.sum for store@v1.3.0"
```

(The proxy can lag a minute after the push; `GOPROXY=direct` fetches straight from GitHub.)

## Consuming a released module

Applications depend on exactly the provider they import; nothing else is pulled:

```bash
go get go.arpabet.com/store/providers/badger@v1.3.0
```

```go
import (
    "go.arpabet.com/store"
    badgerstore "go.arpabet.com/store/providers/badger"
)
```

## Undoing a release

Tags are cheap to delete **before** anyone has fetched them, but the module proxy
caches versions permanently once requested — prefer rolling forward with a new
patch over deleting a published tag.

```bash
# delete a local + remote tag (only safe if not yet consumed)
git tag -d providers/badger/v1.3.0
git push origin :refs/tags/providers/badger/v1.3.0
```

If a release is broken after it has been fetched anywhere, cut a fixed patch
(`v1.3.1`) instead.
