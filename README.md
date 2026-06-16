# compose-envkit

A small **Go CLI** (`cenvkit`) that adds a **Docker Compose env-chain + service
`env_file:` discovery + a provenance debug flow** to any project, built on
Docker's own `compose-go` loader.

It closes one specific gap that native Docker Compose leaves open: a service's
`env_file:` paths populate the **container runtime** environment, but they do
**not** participate in compose-time `${VAR}` interpolation of the YAML itself.
compose-envkit auto-discovers those `env_file:` paths and feeds them back into
interpolation, so `ports: "${APP_PORT:-3000}:80"` resolves to the `APP_PORT`
your service `env_file:` actually defines instead of silently falling back to
`3000`. (That's the whole reason the kit exists — see
[The gap](#the-gap-native-compose-doesnt-close).)

**`cenvkit` is a Go CLI** built on Docker's own compose loader
(`compose-spec/compose-go`). It is the only implementation — the original
POSIX-`sh` engine has been removed. The Go CLI needs only a Go toolchain (no
Python, no Node).

---

## Install — cenvkit (the Go CLI, v1 · current)

`cenvkit` is built on Docker's own loader (`compose-spec/compose-go`, pinned
v2.11.0). Two distribution modes:

```sh
# Installed (recommended)
go install github.com/compose-envkit/compose-envkit/cmd/cenvkit@latest
# or ephemeral: go run github.com/compose-envkit/compose-envkit/cmd/cenvkit@latest <args>
```

In your project:

```sh
cenvkit init               # seed .X from example.X (no-clobber), fan out one level
cenvkit env-files          # the resolved COMPOSE_ENV_FILES chain, one path/line
cenvkit compose config     # interpolation now includes the env_file: layer
cenvkit env-debug --chain  # inspect the chain
```

Or **vendor** it: commit the Go module + the POSIX `cenvkit` shim and run
`./cenvkit <args>` (needs a Go toolchain; for speed, `go build -o .cenvkit.bin
./cmd/cenvkit` — gitignored — and run that). Full command + behavior reference:
**[`docs/cenvkit.md`](docs/cenvkit.md)**.

---

## What it is, and why

Modern Docker Compose already handles most of what a hand-rolled wrapper used to
do — `COMPOSE_ENV_FILES` (last-wins project chain), `COMPOSE_FILE` overlays,
`DOCKER_DEFAULT_PLATFORM`. compose-envkit wraps those for ergonomics, but it
exists for the **one** job that has no native equivalent.

### The gap native compose doesn't close

Docker Compose keeps two things deliberately separate:

| Layer | Populates | Used for compose-time `${VAR}` in the YAML? |
|---|---|---|
| Project-level env (`--env-file` / `COMPOSE_ENV_FILES`) | interpolation context | **yes** |
| A service's `env_file:` | the container's runtime env | **no** |

So if `APP_PORT` lives only in a service's `env_file:` and you write
`ports: "${APP_PORT:-3000}:80"`, compose interpolates `${APP_PORT}` **before**
that `env_file:` is ever read — you silently get the `:-3000` fallback. This is
an intentional, long-standing design split upstream
([docker/compose#3435](https://github.com/docker/compose/issues/3435), open
since 2016).

`cenvkit` loads the real, include-aware compose model (via Docker's own
`compose-go` loader), enumerates each active service's resolved `env_file:`
paths, and folds them into `COMPOSE_ENV_FILES` **in addition** to the project
chain. Now the same files that configure the container at runtime also feed
compose-time interpolation, last-wins. That's Layer 2. Full reference:
[`docs/cenvkit.md`](docs/cenvkit.md).

```sh
cenvkit env-files       # the resolved COMPOSE_ENV_FILES, including the env_file: layer
cenvkit compose config  # interpolation now sees that layer
```

---

## The env-chain

Two layers feed `COMPOSE_ENV_FILES`, in order, last-wins:

**Layer 1 — the project chain.** Listed in `.docker-env-chain` (or built-in
defaults when that file is absent). The default chain is:

```
.env             # non-secret defaults (committed via example.env)
.${ENV}.env      # per-environment overlay: .dev.env / .prod.env / …
.secrets.env     # secrets, gitignored — loaded LAST so it wins
```

`${ENV}` (alias `${COMPOSE_ENV}`) is resolved as **shell `COMPOSE_ENV`
> `.env`'s `COMPOSE_ENV` > `"dev"`**. Non-existent files are silently skipped.

**Layer 2 — service `env_file:` paths.** Enumerated from the real, include-aware
compose model (`compose-go`): the active services' resolved, absolute `env_file:`
paths, normalized and de-duplicated, then appended after Layer 1. These files are
already declared in your YAML — `cenvkit` simply also makes them visible to
interpolation.

```
  shell COMPOSE_ENV / .env / "dev"  ─┐
                                     ├─►  ${ENV} substitution in .docker-env-chain
  .docker-env-chain (Layer 1)  ──────┘            │
                                                  ▼
  compose model env_file: (Layer 2)  ──►  COMPOSE_ENV_FILES  ──►  docker compose
```

See `cenvkit env-files` for the exact resolved list in your project.

---

## Run from root AND from a subproject

`cenvkit` resolves everything relative to its **project directory**: the current
directory by default, or `--project-dir <dir>` to point elsewhere. All env-chain
resolution and `env_file:` enumeration happen relative to that directory — so
running from the repo root and running from a subproject each resolve their own
files correctly.

```sh
cenvkit env-files                    # resolve from the current directory
cd web && cenvkit compose config     # resolve web/'s own chain + env_file:
cenvkit --project-dir web env-files  # same, without changing directory
```

### Monorepo — root orchestrates subprojects

The flip side of subproject *isolation* is the **unified stack**: a root compose
that `include:`s each subproject, run as one stack from the root. Because
`cenvkit` loads the real, include-aware compose model, Layer-2 enumeration
reaches **across** the `include:`, so a `${WEB_PORT}` declared only in
`web/.web.env` resolves even when you render the config from the root — native
compose lands on the `:-0` fallback there.

Runnable blueprint: [`examples/monorepo/`](examples/monorepo/).

---

## The debug flow at a glance

`cenvkit env-debug` is **provenance-backed and daemon-free**: it loads the
compose model in-process (compose-go), with no `docker compose` shell-out, and
never hardcodes your variable or service names. Add `--json` to any mode for the
structured report (tooling/CI).

| Mode | What it shows |
|---|---|
| `--chain` (default) | the Layer-1 chain files, in load order (secrets last) |
| `--files` | the full merged `COMPOSE_ENV_FILES` (Layer 1 + Layer 2) |
| `--effective [--service S]` | each service's effective env, with the source of every value (`env_file:` vs inline `environment:`) |
| `--trace --var NAME` | NAME's winning value, the file that set it, the files it shadowed, and where `${NAME}` took effect (service/field → resolved value) |
| `--value --var NAME` | NAME's winning value, one line (for scripts) |

Full reference: [`docs/cenvkit.md`](docs/cenvkit.md).

```sh
cenvkit env-debug                              # the chain, in load order
cenvkit env-debug --trace --var DATABASE_HOST  # who set/shadowed DATABASE_HOST
cenvkit env-debug --effective --service web     # final values compose will use for web
cenvkit env-debug --trace --var APP_PORT --json # the whole resolution, as JSON
```

---

## Requirements

- **A Go toolchain** to install or build `cenvkit` (`go install` /
  `go run …@latest`, or `go build` in vendored mode). No Python, no Node.
- **Docker Compose** for the compose-touching commands (`cenvkit compose …`,
  `cenvkit validate`). The `env-debug` provenance modes load the model
  in-process via compose-go and need **no** running Docker daemon.
- **Cross-platform:** `cenvkit` is a pure-Go binary — it runs natively on Linux,
  macOS, and Windows.

---

## Layout

```
compose-envkit/
├── cmd/cenvkit/         # the Go CLI entry (cobra)
├── internal/
│   ├── chain/           # Layer 1 — the .docker-env-chain project chain (pure Go)
│   ├── engine/          # Layer 2 — env_file: enumeration (the only compose-go importer)
│   ├── envfiles/        # merge / order / dedup into COMPOSE_ENV_FILES
│   ├── provenance/      # env-debug provenance model + human/JSON render
│   └── bootstrap/       # cenvkit init
├── cenvkit              # vendored-mode POSIX shim (runs `go run ./cmd/cenvkit`)
├── go.mod / go.sum      # module + compose-go v2.11.0 pin
├── examples/monorepo/   # runnable root-include:s-subprojects blueprint (cenvkit-driven)
├── test/                # acceptance suite driving the cenvkit binary
└── docs/cenvkit.md      # the canonical command + behavior reference
```

---

## Documentation

- [`docs/cenvkit.md`](docs/cenvkit.md) — **the canonical reference — start
  here**: install (both modes), the two layers, every command, the `env-debug`
  provenance modes, and the behavior contracts (D1 missing-`env_file:`, variable
  precedence, the `env_file:`-path model, `COMPOSE_DEPTH` accepted-but-ignored,
  no over-discovery).
- [`docs/superpowers/`](docs/superpowers/) — the design spec and implementation
  plans (historical record).
- [`AGENTS.md`](AGENTS.md) — integration guide for AI agents.
- [`examples/monorepo/`](examples/monorepo/) — runnable, cenvkit-driven
  root-`include:`s-subprojects blueprint.

---

## License

MIT — see [`LICENSE`](LICENSE).
