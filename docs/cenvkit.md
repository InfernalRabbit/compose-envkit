# cenvkit — the Go CLI

`cenvkit` is the implementation of compose-envkit: a small Go CLI built on
**Docker's own compose loader** (`github.com/compose-spec/compose-go/v2`, pinned
`v2.11.0`). It assembles `COMPOSE_ENV_FILES` from your **project env chain** and
`exec`s `docker compose`, and ships a **daemon-free debugger** that explains where
every variable comes from — and flags the one place Compose silently bites you.

> This is the **one-page reference**. For the full end-to-end guide (setup,
> monorepos, CI, troubleshooting, worked examples), see [`guide.md`](guide.md).

## What it does, and the gap it surfaces

Docker Compose keeps two layers deliberately separate:

| Layer | Populates | Visible to compose-time `${VAR}` in the YAML? |
|---|---|---|
| Project env (`COMPOSE_ENV_FILES` / `--env-file` / `.env`) | the interpolation context | **yes** |
| A service's `env_file:` | the container's runtime env | **no** |

`cenvkit`:

1. **Assembles `COMPOSE_ENV_FILES` from your project env chain (Layer 1)** —
   tokenized, ordered, secrets-last — so `docker compose` interpolates `${VAR}`
   against it. This is the **run path** (`cenvkit compose`/`env-files`).
2. **Keeps a service `env_file:` runtime-only** — exactly Docker's native
   semantics, per-service, isolated. cenvkit does **not** fold it into
   interpolation.
3. **`env-debug` detects and explains the gap**: when a `${VAR}` in your YAML is
   satisfied *only* by a service `env_file:`, the real run falls back to the
   `:-default`. The debugger tells you exactly that — and how each service's
   container env actually resolves — entirely in-process (no daemon).

> **History.** Earlier cenvkit (and the legacy sh kit) folded every service
> `env_file:` into `COMPOSE_ENV_FILES` to *close* the gap
> ([docker/compose#3435](https://github.com/docker/compose/issues/3435)). That is
> **reversed** (Layer-2-debug-only, 2026-06-17): flattening every service's
> env_file into one global interpolation namespace collapsed shared keys (a
> `${PORT}` collision footgun) and was the wrong default. `env_file:` is now
> runtime-only; the gap is **surfaced** by the debugger, not silently patched at
> run time. To make a var feed interpolation, put it in the Layer-1 chain.

Built on the real loader (not the legacy `awk`/`sed`): no glob over-discovery (the
`include:` graph is authoritative), `${SVC_DIR}` and nested `${A:-${B:-c}}`
resolve, no `sed`-injection vector (pure Go strings; host/env tokens whitelisted
to `[A-Za-z0-9._-]`).

> The legacy POSIX-`sh` kit (`bin/`, `lib/`, `mk/`) has been **removed**.
> `cenvkit` is the only implementation.

## How it works

1. **Layer 1 — the chain** (`internal/chain`, pure Go). Reads `.docker-env-chain`
   (or the default `.env → .${COMPOSE_ENV}.env → .secrets.env`), substitutes
   `${ENV}`/`${COMPOSE_ENV}`/`${HOST}`/`${HOSTNAME}` (sanitized), keeps the files
   that exist. **This — and only this — is `COMPOSE_ENV_FILES` for the run**: the
   interpolation context.
2. **Service `env_file:` enumeration** (`internal/engine`, the only package
   importing compose-go). Loads the real project (`COMPOSE_FILE`, `include:`,
   profiles, seeded with Layer-1) and enumerates the active services' resolved
   `env_file:` paths. **This is used only by `env-debug`** (the gap-detector and
   the `--files` runtime-only view) — it is **not** appended to the run
   `COMPOSE_ENV_FILES`.

## Install

**Installed** (recommended):

```sh
go install github.com/InfernalRabbit/compose-envkit/cmd/cenvkit@latest
# ephemeral, no install:
go run github.com/InfernalRabbit/compose-envkit/cmd/cenvkit@latest <args>
```

**Vendored** (commit the Go module + the POSIX `cenvkit` shim; requires a Go
toolchain — no committed binaries, no network):

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
| `cenvkit compose <args>` | assemble the Layer-1 chain, `exec docker compose <args>` (the core) |
| `cenvkit env-files` | print the resolved `COMPOSE_ENV_FILES` (**Layer 1 only**), one path per line |
| `cenvkit env-debug [--trace --var V\|--effective [--service S]\|--value --var V\|--chain\|--files\|--overview] [--json]` | daemon-free debugger / gap-detector |
| `cenvkit validate [--all]` | `docker compose config -q`; `--all` validates dev AND prod |
| `cenvkit init` | seed `.X` from `example.X` (**no-clobber**), fan out one directory level |
| `cenvkit version` | print the version |

Global: `--project-dir <dir>` (default: current directory).

`env-debug` is **daemon-free** — it loads the model in-process via compose-go, with
NO `docker compose` shell-out:

- `--trace --var V` — if `V` is in the chain: its winning value, the file that set
  it, the shadowed files, and where `${V}` took effect (service/field → resolved).
  If `V` is **not** in the chain but is defined in a service `env_file:`: the
  **gap** — `${V}` falls back at run time, the runtime def(s), the affected
  fields, and a fix hint.
- `--effective [--service S]` — each service's **final container env** with the
  source of every value (`env_file:` vs inline `environment:`). Inline
  `environment:` is interpolated against **Layer 1** (so an inline
  `${env_file_only_var}` shows its fallback — the true container value); `env_file:`
  entries are verbatim; inline overrides `env_file:`.
- `--value --var V` — `V`'s interpolation value (one line; empty if env_file-only).
- `--chain` (default) — the Layer-1 chain files (secrets last). `--files` — **two
  groups**: *interpolation* (= `COMPOSE_ENV_FILES`, Layer 1) and *runtime-only*
  (each service's declared `env_file:` paths, NOT interpolated).
- `--overview` — a per-file **layering** view: the interpolation chain walked file
  by file with markers `+` new / `~` override (`old → new`) / `·` repeat (raw
  literal values, `${...}` unexpanded), then per-service `env_file:` layers + an
  `inline environment:` final layer (inline wins), with `⚠ gap:` lines for vars
  referenced as `${VAR}` but defined only in a service `env_file:`. Restores the
  sh kit's `env-debug-diff` overview, correct under v3.
- `--json` — the structured provenance `Report` for any mode (tooling/CI);
  `--overview --json` adds the `layers` array.

`--trace` effects are reported for **service fields only** (`services.<name>.<field>`);
`${VAR}` in top-level `networks:`/`volumes:`/`x-*` is out of scope (the var still
appears in chain attribution if it is a chain key). The `--effective` view never
reports a resolution the real run won't produce. Provenance uses compose-go's own
`dotenv` + `template` packages (docker-compose parity).

## Behavior contracts (what to rely on)

- **`env_file:` is runtime-only.** A `${VAR}` defined *only* in a service
  `env_file:` is **not** resolved at the run — `docker compose` interpolates the
  `:-default` (native fallback). `env-debug --trace` flags it as a gap. To make a
  var feed `${VAR}` interpolation, put it in the **Layer-1 chain**.
- **Run path = Layer 1 only.** `COMPOSE_ENV_FILES` (what `env-files`/`compose`
  set) never contains a service `env_file:` path.
- **Missing `env_file:` (D1).** A missing *required* `env_file:` is lenient at
  enumeration (so `env-debug` never aborts) and **upstream-fatal at the real
  `docker compose` run** (which re-enforces `required:`). cenvkit does not
  reimplement upstream's `required:` semantics.
- **Secrets-last.** A guarantee **within the Layer-1 chain** (`.secrets.env` is
  emitted after the other Layer-1 files; last-wins). Because a service `env_file:`
  no longer feeds interpolation, it can **no longer clobber a chain/secret var at
  interpolation time**. (At runtime each `env_file:` applies only to its own
  container, natively.)
- **`COMPOSE_FILE` overlays** like `docker-compose.yml:docker-compose.${COMPOSE_ENV}.yml`
  work: cenvkit interpolates `${COMPOSE_ENV}`/`${ENV}` and splits on
  `COMPOSE_PATH_SEPARATOR` (else the OS path-list separator; never `,`).
- **`COMPOSE_DEPTH`** is accepted-but-ignored (the `include:` graph makes
  depth-bounded glob discovery obsolete). **No over-discovery** — a compose file
  not in the `include:` graph / `COMPOSE_FILE` is not loaded; `.git` is irrelevant.

## Configuration

- `.docker-env-chain` — the Layer-1 chain (one path template per line; `#`
  comments and blank lines ignored). Back-compatible with the sh kit's format.
- `COMPOSE_ENV` — selects the env tier (shell > `.env`'s `COMPOSE_ENV=` > `dev`).
- `COMPOSE_FILE`, `COMPOSE_PROFILES`, `HOSTNAME`/`HOST` — honored as above.

## Architecture / contributing

- `cmd/cenvkit` — cobra CLI. `internal/chain` — Layer 1 (the run
  `COMPOSE_ENV_FILES`). `internal/engine` — service `env_file:` enumeration for
  `env-debug` (the **only** package importing compose-go; a CI seam check enforces
  this). `internal/envfiles` — merge/order/dedup. `internal/provenance` —
  env-debug model + human/JSON render incl. the gap-detector (pure Go; `engine`
  imports it). `internal/bootstrap` — `cenvkit init`.
- Upstream-first: compose-go is the source of truth for compose semantics; the
  version is pinned and bumped deliberately (re-run acceptance on a bump — see the
  spec §7 checklist).
- Acceptance is the ported `examples/monorepo` smoke suite (`test/`); design +
  rationale in
  [`docs/superpowers/specs/2026-06-17-cenvkit-layer2-debug-only-design.md`](superpowers/specs/2026-06-17-cenvkit-layer2-debug-only-design.md)
  (current run-path contract; supersedes the 2026-06-15 / 2026-06-16 specs for the
  env-file model).
