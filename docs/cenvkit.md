# cenvkit — the Go CLI (v1)

`cenvkit` is the current implementation of compose-envkit: a small Go CLI built
on **Docker's own compose loader** (`github.com/compose-spec/compose-go/v2`,
pinned `v2.11.0`). It assembles `COMPOSE_ENV_FILES` from the real, include-aware,
interpolated compose model and then `exec`s `docker compose`.

It closes the same gap the original POSIX-`sh` kit did — a service `env_file:`
populates the *container* environment but is invisible to compose-time `${VAR}`
interpolation — but by importing the real loader rather than a hand-rolled
`awk`/`sed` parser. That removes the whole legacy bug class: no glob
over-discovery (the `include:` graph is authoritative), `${SVC_DIR}` and nested
`${A:-${B:-c}}` resolve, and there is no `sed`-injection vector (pure Go strings,
host/env tokens whitelisted to `[A-Za-z0-9._-]`).

> The legacy POSIX-`sh` kit (`bin/docker`, `lib/`, `mk/`, `scripts/`,
> `install.sh`) is **deprecated** and retained one release as the parity
> reference. See [integration.md](integration.md) for it.

## How it works (the two layers)

1. **Layer 1 — the chain** (`internal/chain`, pure Go). Reads `.docker-env-chain`
   (or the built-in default `.env → .${COMPOSE_ENV}.env → .secrets.env`),
   substitutes the tokens `${ENV}`/`${COMPOSE_ENV}`/`${HOST}`/`${HOSTNAME}`
   (host/env sanitized), keeps the files that exist (missing files are skipped),
   and builds the merged `K=V` seed environment.
2. **Layer 2 — service `env_file:` enumeration** (`internal/engine`, the only
   package importing compose-go). Loads the project (honoring `COMPOSE_FILE`,
   `include:`, profiles, interpolation, seeded with the Layer-1 vars) and
   enumerates the **active** services' resolved, absolute `env_file:` paths.

The two layers merge into one ordered, deduped `COMPOSE_ENV_FILES`
(`<Layer 1 in chain order, secrets last within Layer 1>, <Layer 2>`), which the
real `docker compose` then loads as its interpolation context.

## Install

**Installed** (recommended):

```sh
go install github.com/compose-envkit/compose-envkit/cmd/cenvkit@latest
# ephemeral, no install:
go run github.com/compose-envkit/compose-envkit/cmd/cenvkit@latest <args>
```

**Vendored** (commit the Go module + the POSIX `cenvkit` shim into your project;
requires a Go toolchain — no committed binaries, no network):

```sh
./cenvkit <args>          # the shim runs `go run ./cmd/cenvkit`
```

For lower per-invocation latency in vendored mode, build once into a gitignored
local binary and run that:

```sh
go build -o .cenvkit.bin ./cmd/cenvkit   # .cenvkit.bin is gitignored
./.cenvkit.bin compose up
```

## Commands

| Command | What it does |
|---|---|
| `cenvkit compose <args>` | assemble the chain, `exec docker compose <args>` (the core) |
| `cenvkit env-files` | print the resolved `COMPOSE_ENV_FILES` chain, one path per line |
| `cenvkit env-debug [--chain\|--files\|--value --var V\|--trace --var V\|--diff\|--effective]` | inspect the chain |
| `cenvkit validate [--all]` | `docker compose config -q`; `--all` validates dev AND prod |
| `cenvkit init` | seed `.X` from `example.X` (**no-clobber**), fan out one directory level |
| `cenvkit version` | print the version |

Global: `--project-dir <dir>` (default: current directory).

`env-debug` notes: `--value`/`--trace` read raw last-wins literals from the
resolved files (`--value` is scoped to the Layer-1 chain; `--trace` spans the
merged set). They do **not** expand `${VAR:-default}` — for fully-resolved values
use `--effective` (which runs `docker compose config`).

## Behavior contracts (what to rely on)

- **Missing `env_file:` (D1).** A missing *required* `env_file:` is **lenient at
  chain assembly** (the enumeration pass skips it, so the chain never aborts) and
  **upstream-fatal at the real `docker compose` run** (which re-enforces
  `required:`). cenvkit does not reimplement upstream's `required:` semantics.
- **Variable precedence (W3 / §4c).** `docker compose` owns variable precedence
  (last-wins over `COMPOSE_ENV_FILES`). cenvkit only controls the **file order**.
  "Secrets last" is a guarantee **within the Layer-1 chain** (`.secrets.env` is
  emitted after the other Layer-1 files). A Layer-2 *service* `env_file:` is
  emitted after Layer 1, so if it reuses a chain variable name it will win at
  load time — **do not reuse secret variable names in service `env_files`.**
- **`env_file:` paths (§4a).** A service `env_file:` *path* may reference only
  Layer-1 / project-chain vars (e.g. `env_file: ${SVC_DIR}/.env` where `SVC_DIR`
  is in `.env`). A path that depends on a var defined only inside another
  service's Layer-2 `env_file:` is unsupported in v1 (single-pass) — it is not
  silently mis-resolved.
- **`COMPOSE_DEPTH`** is **accepted-but-ignored** — the `include:` graph makes the
  old depth-bounded glob discovery obsolete. Setting it is tolerated (no error)
  for back-compat.
- **No over-discovery.** A compose file that is not in the `include:` graph or
  `COMPOSE_FILE` is not loaded (the legacy kit's filename-glob over-discovery is
  gone). `.git` presence is irrelevant to discovery.
- **`COMPOSE_FILE` overlays** like `docker-compose.yml:docker-compose.${COMPOSE_ENV}.yml`
  work: cenvkit interpolates `${COMPOSE_ENV}`/`${ENV}` and splits on
  `COMPOSE_PATH_SEPARATOR` (else the OS path-list separator; never `,`).

## Configuration

- `.docker-env-chain` — the Layer-1 chain (one path template per line; `#`
  comments and blank lines ignored). Back-compatible with the sh kit's format.
- `COMPOSE_ENV` — selects the env tier (shell > `.env`'s `COMPOSE_ENV=` > `dev`).
- `COMPOSE_FILE`, `COMPOSE_PROFILES`, `HOSTNAME`/`HOST` — honored as above.

## Architecture / contributing

- `cmd/cenvkit` — cobra CLI. `internal/chain` — Layer 1. `internal/engine` — Layer
  2 (the **only** package importing compose-go; a CI seam check enforces this).
  `internal/envfiles` — merge/order/dedup. `internal/debug` — env-debug.
  `internal/bootstrap` — `cenvkit init`.
- Upstream-first: compose-go is the source of truth for compose semantics; the
  version is pinned and bumped deliberately (re-run acceptance on a bump — see the
  spec §10 checklist).
- Acceptance is the ported `examples/monorepo` smoke suite (`test/`), driving the
  `cenvkit` binary; design + rationale in
  [`docs/superpowers/specs/2026-06-15-cenvkit-go-rewrite-design.md`](superpowers/specs/2026-06-15-cenvkit-go-rewrite-design.md).
