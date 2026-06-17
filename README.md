# compose-envkit

A small **Go CLI** (`cenvkit`) that adds a **Docker Compose env-chain + a
daemon-free provenance / gap debugger** to any project, built on Docker's own
`compose-go` loader.

It manages the project env-chain that drives compose-time `${VAR}` interpolation,
keeps each service's `env_file:` **runtime-only** (native Docker semantics), and
**surfaces the gap** native Compose leaves open: a value defined only in a service
`env_file:` is invisible to `${VAR}` interpolation, so `ports: "${APP_PORT:-3000}:80"`
silently falls back to `3000`. cenvkit's `env-debug` detects and explains exactly
that. (See [The gap](#the-gap-native-compose-doesnt-close).)

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
go install github.com/InfernalRabbit/compose-envkit/cmd/cenvkit@latest
# or ephemeral: go run github.com/InfernalRabbit/compose-envkit/cmd/cenvkit@latest <args>
```

In your project:

```sh
cenvkit init               # seed .X from example.X (no-clobber), fan out one level
cenvkit env-files          # the resolved COMPOSE_ENV_FILES chain, one path/line
cenvkit compose config     # render config (interpolation = your Layer-1 chain)
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

`cenvkit` does **not** paper over this by folding service `env_file:`s into
interpolation — doing so flattens every service's env into one global namespace (a
`${PORT}` collision footgun, since Compose interpolates the whole YAML against one
env map). Instead it keeps `env_file:` **runtime-only** and gives you a daemon-free
**gap-detector**: `env-debug` tells you when a `${VAR}` is satisfied only by a
service `env_file:` (so the run falls back), shows the runtime value, and
recommends the fix — **put values you reference as `${VAR}` into the Layer-1
chain.** Full reference: [`docs/cenvkit.md`](docs/cenvkit.md).

```sh
cenvkit env-files                          # the resolved COMPOSE_ENV_FILES (Layer-1 chain)
cenvkit env-debug --trace --var APP_PORT   # in the chain, or an env_file gap?
```

---

## The env-chain

**The project chain is `COMPOSE_ENV_FILES`** — the interpolation context. Listed
in `.docker-env-chain` (or built-in defaults when that file is absent). The default
chain is:

```
.env             # non-secret defaults (committed via example.env)
.${ENV}.env      # per-environment overlay: .dev.env / .prod.env / …
.secrets.env     # secrets, gitignored — loaded LAST so it wins
```

`${ENV}` (alias `${COMPOSE_ENV}`) is resolved as **shell `COMPOSE_ENV`
> `.env`'s `COMPOSE_ENV` > `"dev"`**. Non-existent files are silently skipped.

**Service `env_file:` is runtime-only** — it configures the container, not
interpolation, and is **not** added to `COMPOSE_ENV_FILES`. cenvkit still loads the
real, include-aware model and enumerates those paths, but only to power `env-debug`
(the gap-detector and the `--files` runtime-only view).

```
  shell COMPOSE_ENV / .env / "dev"  ─┐
                                     ├─►  ${ENV} substitution
  .docker-env-chain (Layer 1)  ──────┴─►  COMPOSE_ENV_FILES  ──►  docker compose
                                                                  (interpolation)
  service env_file:  ──►  container runtime env only  +  env-debug gap-detector
```

See `cenvkit env-files` for the chain, and `cenvkit env-debug --files` for the
runtime-only env_file paths.

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
that `include:`s each subproject, run as one stack from the root. cenvkit loads the
real, include-aware model, so `env-debug` sees the whole project. Note the rule: a
`${WEB_PORT}` declared only in `web/.web.env` (a service `env_file:`) **falls back**
at the root — `env_file:` is runtime-only. `cenvkit env-debug --trace --var WEB_PORT`
flags the gap; promote `WEB_PORT` to the Layer-1 chain if you need it interpolated.

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
| `--files` | two groups: interpolation (`COMPOSE_ENV_FILES`, Layer 1) + runtime-only (service `env_file:` paths) |
| `--overview` | per-file layering walk (`+`/`~`/`·` markers, raw values) + per-service `env_file:` layers + `inline environment:`, with `⚠ gap` lines |
| `--effective [--service S]` | each service's effective env, with the source of every value (`env_file:` vs inline `environment:`) |
| `--trace --var NAME` | NAME's chain winner + where `${NAME}` took effect — or the **gap** (NAME is only in a service `env_file:`, so the run falls back) |
| `--value --var NAME` | NAME's winning value, one line (for scripts) |

Output is **colored** on a terminal (markers, headers, gaps) and **plain** when
piped / `--json` / `NO_COLOR` / CI — control with `--color=auto|always|never`.

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
│   ├── engine/          # service env_file: enumeration for env-debug (the only compose-go importer)
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

- [`docs/guide.md`](docs/guide.md) — **the full user guide — start here**:
  install, the env-chain (Layer 1/2), every command with worked examples,
  monorepos, `env-debug` provenance, CI, the behavior contracts, and
  troubleshooting.
- [`docs/cenvkit.md`](docs/cenvkit.md) — the one-page reference (commands +
  behavior contracts at a glance).
- [`docs/superpowers/`](docs/superpowers/) — the design spec and implementation
  plans (historical record).
- [`AGENTS.md`](AGENTS.md) — integration guide for AI agents.
- [`examples/monorepo/`](examples/monorepo/) — runnable, cenvkit-driven
  root-`include:`s-subprojects blueprint.

---

## License

MIT — see [`LICENSE`](LICENSE).
