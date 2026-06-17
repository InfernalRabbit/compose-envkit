# cenvkit — User Guide

A complete, end-to-end guide to `cenvkit`: what it does, how to install it, how to
set up a project, every command, monorepos, the env-debug **gap-detector**, CI, the
behavior contracts you can rely on, and troubleshooting.

For the terse reference, see [`cenvkit.md`](cenvkit.md). For the design rationale,
see [`superpowers/specs/`](superpowers/specs/) (current model:
`2026-06-17-cenvkit-layer2-debug-only-design.md`).

---

## 1. What it is, and the gap it surfaces

`cenvkit` is a small Go CLI built on **Docker's own compose loader**
(`github.com/compose-spec/compose-go/v2`). It assembles `COMPOSE_ENV_FILES` from
your **project env chain** and then `exec`s `docker compose` — and it ships a
daemon-free debugger that explains where every variable resolves.

**Two layers, deliberately separate (this is Docker, not cenvkit):**

| Layer | Populates | Visible to compose-time `${VAR}` in the YAML? |
|---|---|---|
| Project env chain (`COMPOSE_ENV_FILES` / `--env-file` / `.env`) | the interpolation context | **yes** |
| A service's `env_file:` | the container's runtime env | **no** |

So if `APP_PORT` lives only in a service's `env_file:` and your YAML says
`ports: "${APP_PORT:-3000}:80"`, compose interpolates `${APP_PORT}` **before** it
reads that `env_file:` — you silently get the `:-3000` fallback. (Upstream
[docker/compose#3435](https://github.com/docker/compose/issues/3435), open since
2016.)

**What cenvkit does about it:**

1. It assembles `COMPOSE_ENV_FILES` from your **project chain** (Layer 1) — the
   right place for values that *should* feed `${VAR}` interpolation across the
   project.
2. It keeps each service's `env_file:` **runtime-only** — exactly Docker's native
   behavior (per-service, isolated). cenvkit does **not** fold it into
   interpolation.
3. Its `env-debug` **detects and explains the gap**: when a `${VAR}` is satisfied
   only by a service `env_file:`, it tells you the run will fall back, shows the
   runtime value, and tells you how to fix it.

> **Why not just close the gap by folding env_files into interpolation?** Earlier
> versions did exactly that. But a service `env_file:` is *per-service*, and
> folding every service's file into one flat `COMPOSE_ENV_FILES` collapses a shared
> key (`${PORT}`) into a single **global** interpolation value across all services
> — a real collision footgun, because Compose interpolates the whole YAML against
> one global env map. So the rule is: **values meant for `${VAR}` interpolation
> belong in the Layer-1 chain; `env_file:` is for the container.** cenvkit surfaces
> the gap instead of silently papering over it.

---

## 2. Installation

**Installed (recommended).** Needs a Go toolchain:

```sh
go install github.com/InfernalRabbit/compose-envkit/cmd/cenvkit@latest
cenvkit version
```

**Ephemeral** (no install — npx-style):

```sh
go run github.com/InfernalRabbit/compose-envkit/cmd/cenvkit@latest env-files
```

**Vendored** (commit the module + a POSIX `cenvkit` shim into your repo; needs a
Go toolchain, no committed binaries, no network):

```sh
./cenvkit <args>          # the shim runs `go run ./cmd/cenvkit "$@"`
```

For lower per-invocation latency in vendored mode, build once into a gitignored
local binary:

```sh
go build -o .cenvkit.bin ./cmd/cenvkit   # .cenvkit.bin is gitignored
./.cenvkit.bin compose up
```

**Runtime needs:** `docker compose` (≥ 2.24) for the compose-touching commands
(`compose`, `validate`). The `env-debug` modes are **daemon-free** — they need no
Docker.

---

## 3. Quick start

From your project directory:

```sh
# 1. Seed env files from the committed example.* templates (never clobbers).
cenvkit init
#    -> creates .env, .dev.env, .secrets.env, ... from example.env, example.dev.env, ...

# 2. Fill in values (real secrets go in .secrets.env, which stays gitignored).
#    Put anything you reference as ${VAR} in your compose YAML into the chain
#    (.env / .dev.env / .secrets.env) — NOT into a service env_file.
$EDITOR .env .secrets.env

# 3. See the resolved chain (one path per line, in load order).
cenvkit env-files

# 4. Render the compose config.
cenvkit compose config

# 5. Anything you'd run with `docker compose`, run with `cenvkit compose`.
cenvkit compose up -d
cenvkit compose logs -f web

# 6. When a value surprises you, ask why (and catch the env_file gap).
cenvkit env-debug --trace --var APP_PORT
```

The golden rule: **in an integrated project, go through `cenvkit compose`** so the
Layer-1 chain (tokens, ordering, secrets-last, `COMPOSE_FILE` overlay) is applied
consistently. And: **if you need a value at `${VAR}` interpolation time, put it in
the chain** — a service `env_file:` only configures that container.

---

## 4. The env-chain vs. service env_files

There are two distinct things, and cenvkit keeps them distinct.

### 4.1 The project chain (Layer 1) — this is `COMPOSE_ENV_FILES`

Listed in `.docker-env-chain` (one path template per line; `#` comments and blank
lines ignored). If that file is absent, the built-in default is:

```
.env             # non-secret defaults (committed via example.env)
.${COMPOSE_ENV}.env   # per-environment overlay: .dev.env / .prod.env / …
.secrets.env     # secrets, gitignored — listed LAST so it wins (last-wins)
```

A typical explicit chain with a per-machine layer:

```
.env
.${ENV}.env
.${HOSTNAME}.env
.secrets.env
```

**This — and only this — becomes `COMPOSE_ENV_FILES`**, the context
`docker compose` uses to interpolate `${VAR}` in your YAML.

**Tokens** substituted in each entry: `${ENV}` and `${COMPOSE_ENV}` → the resolved
env; `${HOST}` and `${HOSTNAME}` → the machine hostname. Both are **sanitized** to
`[A-Za-z0-9._-]` (a hostname with `|`/`&`/`,` cannot inject a path or split the
file list). Non-existent files are silently skipped.

- **`COMPOSE_ENV` resolution:** shell `COMPOSE_ENV` > a `COMPOSE_ENV=` line in
  `.env` > `"dev"`.
- **`HOST`/`HOSTNAME`:** exported `HOSTNAME` > the `hostname` command.

```sh
$ cenvkit env-files
/app/.env
/app/.dev.env
```

### 4.2 Service `env_file:` — runtime-only

A service's `env_file:` configures **that service's container** at runtime
(per-service, isolated) — native Docker. cenvkit does **not** add it to
`COMPOSE_ENV_FILES`, so it does **not** feed `${VAR}` interpolation. If a `${VAR}`
in your YAML is satisfied only by a service `env_file:`, the run falls back to the
`:-default` (see §7 to detect this).

cenvkit still *knows* about service env_files (it loads the real, include-aware
model) — but only to power `env-debug` (the gap-detector and the `--files`
runtime-only view), never the run.

---

## 5. Commands

Global flag: `--project-dir <dir>` — the project directory (default: the current
directory). Everything resolves relative to it.

| Command | What it does |
|---|---|
| `cenvkit compose <args…>` | assemble the Layer-1 chain, set `COMPOSE_ENV_FILES`, `exec docker compose <args…>` |
| `cenvkit env-files` | print the resolved `COMPOSE_ENV_FILES` (**Layer 1 only**), one absolute path per line |
| `cenvkit env-debug […] [--json]` | inspect the chain + service env with provenance / gap-detection (in-process, no daemon) — see §7 |
| `cenvkit validate [--all]` | `docker compose config -q`; `--all` validates dev AND prod |
| `cenvkit init` | seed `.X` from `example.X` (**no-clobber**), fan out one directory level |
| `cenvkit version` | print the version |

### `cenvkit compose`

The core. Assembles `COMPOSE_ENV_FILES` (Layer 1) and execs `docker compose`,
passing all arguments through; exit code is propagated.

```sh
cenvkit compose config
cenvkit compose up -d
cenvkit compose --project-dir services/web ps   # run against a subproject
```

### `cenvkit env-files`

```sh
$ cenvkit env-files
/app/.env
/app/.dev.env
```

The exact files (and order) that feed interpolation. Note: **no service env_file
paths** appear here — they are runtime-only (see `env-debug --files` for those).

### `cenvkit validate`

```sh
cenvkit validate          # validate the currently-resolved COMPOSE_ENV
cenvkit validate --all    # validate dev AND prod (re-resolves the chain per env)
```

Non-zero exit on an invalid config. `--all` re-resolves the Layer-1 chain for each
environment (so `.dev.env`/`.prod.env` tiers are picked up correctly).

### `cenvkit init`

Seeds `.<name>` from each `example.<name>` in the project dir **without
clobbering** an existing file (so it never destroys a populated `.secrets.env`),
then fans out one directory level into immediate subprojects. No `sudo`, no
`chmod`, never writes secrets to disk beyond copying the example template.

```sh
$ cenvkit init
# example.env       -> .env        (created)
# example.dev.env   -> .dev.env    (created)
# .secrets.env      -> left untouched (already exists)
# web/example.env   -> web/.env    (created, fan-out)
```

---

## 6. Monorepos

cenvkit loads the **real, include-aware** compose model, so it understands the full
project — but remember the rule from §4: only the **project chain** feeds
interpolation; service `env_file:`s are runtime-only.

### Root orchestrates subprojects (unified stack)

A root `docker-compose.yml` that `include:`s each subproject:

```yaml
# docker-compose.yml
include:
  - ./web
  - ./api
  - ./services/reports
```

If `web/docker-compose.yml` writes `ports: "${WEB_PORT:-0}:80"` and `WEB_PORT`
lives only in `web/.web.env` (a service `env_file:`), then at the run **`${WEB_PORT}`
falls back to `0`** — `env_file:` is runtime-only. The container still receives
`WEB_PORT` from its env_file, but the YAML interpolation does not see it.

**Two correct choices:**

1. **You want `${WEB_PORT}` to interpolate** (e.g. it's a published port): put it
   in the **Layer-1 chain** — the root `.env`, a per-subproject chain file, or
   `.secrets.env`. Then it feeds interpolation everywhere.
2. **It's genuinely runtime-only** (just an env var the container reads): leave it
   in the service `env_file:` and don't reference it as `${...}` in YAML.

Use `env-debug` to see exactly which case you're in (§7):

```sh
$ cenvkit env-debug --files | sed -n '1,12p'
interpolation (COMPOSE_ENV_FILES):
  /app/.env
  /app/.dev.env
runtime-only (service env_file: — NOT interpolated, container env only):
  api:
    /app/api/.api.env
  web:
    /app/web/.web.env
    /app/web/.web.dev.env
```

### Isolated subproject

Run from inside a subproject (or point at it) and it resolves **its own** chain and
service env_file set, blind to siblings:

```sh
cd web && cenvkit compose config      # web/'s own chain
cenvkit --project-dir web env-files   # same, without cd
```

### dev / prod from one knob

Drive overlays with a single `COMPOSE_ENV`. A `COMPOSE_FILE` selector like
`docker-compose.yml:docker-compose.${COMPOSE_ENV}.yml` is interpolated by cenvkit
(see §9), and per-environment chain tiers (`.dev.env`/`.prod.env`) are picked up:

```sh
cenvkit compose config                      # dev (default)
COMPOSE_ENV=prod cenvkit compose config     # prod overlay + .prod.env tiers
```

---

## 7. `env-debug` — provenance & the gap-detector

`env-debug` answers *"why does this variable have this value, where does it take
effect, and what will the container actually get?"* — entirely **in-process (no
Docker daemon)**. Add `--json` to any mode for the structured `Report`.

| Mode | Shows |
|---|---|
| `--chain` (default) | the Layer-1 chain files, in load order (secrets last) |
| `--files` | two groups: **interpolation** (`COMPOSE_ENV_FILES`, Layer 1) + **runtime-only** (service `env_file:` paths, by service) |
| `--trace --var V` | if `V` is in the chain: its winner, shadowed files, and effects. If `V` is env_file-only: the **gap** (falls back at the run) + the runtime def + a fix |
| `--effective [--service S]` | each service's **final container env**, with the source of every value (`env_file:` vs inline `environment:`) |
| `--value --var V` | `V`'s interpolation value, one line (empty if env_file-only) |

### `--trace` — a chain variable (no gap)

```sh
$ cenvkit env-debug --trace --var COMPOSE_PROJECT_NAME
COMPOSE_PROJECT_NAME=monorepo
  winner:     /app/.env (layer1)
```

### `--trace` — the env_file gap

```sh
$ cenvkit env-debug --trace --var WEB_PORT
WEB_PORT
  interpolation: NOT in the Layer-1 chain -> ${WEB_PORT} falls back at run time
  runtime:       /app/web/.web.env -> WEB_PORT=18080  (service `web` container env only)
  ⚠ gap: ${WEB_PORT} used in service web environment[0] resolves to "WEB_PORT=0" at the run, NOT the env_file value (defined only in a service env_file).
  ⚠ gap: ${WEB_PORT} used in service web ports[0] resolves to "0:80" at the run, NOT the env_file value (defined only in a service env_file).
  fix:   add WEB_PORT to the Layer-1 chain (e.g. .env), or use it runtime-only.
```

That tells you: `WEB_PORT` is **not** in the interpolation chain, so every
`${WEB_PORT}` in the YAML falls back at the run; it *is* defined in `web/.web.env`
(value 18080) but that only reaches the `web` container; and the fix is to promote
it to Layer 1 (or accept that it's runtime-only).

### `--effective` — a service's final container env, with sources

```sh
$ cenvkit env-debug --effective --service web
service web:
  IS_DEV=true       <- (inline environment:) (environment)
  STACK_TIER=dev    <- (inline environment:) (environment)
  WEB_DEBUG=true    <- /app/web/.web.dev.env (env_file)
  WEB_PORT=0        <- (inline environment:) (environment)
```

This is the **truth about the container**: `WEB_DEBUG` comes from an env_file;
`WEB_PORT=0` is the inline `environment:` value (interpolated against Layer 1 →
the `:-0` fallback), which **overrides** the `18080` in `web/.web.env`. Inline
`environment:` always wins over `env_file:`, and inline `${VAR}` interpolates
against the chain — so `env-debug` shows what the container *actually* gets, never
a value the run won't produce.

### `--json` — the structured Report

```sh
cenvkit env-debug --trace --var WEB_PORT --json
```

Carries `files` (Layer-1 `COMPOSE_ENV_FILES`), `chain_files`, per-var
`{value, in_chain, winner, overridden, runtime_defs, effects[], gap}`, and
per-service `services[]` (entries with sources + the declared `env_files`). Effects
are reported for **service fields only** (`services.<name>.<field>`).

---

## 8. CI

cenvkit's own CI (a good template) runs, on every push/PR:

```yaml
# .github/workflows/ci.yml (essentials)
jobs:
  lint:
    steps:
      - run: go vet ./...
      - run: gofmt -l . | (! grep .)            # fail if anything is unformatted
      - run: |                                   # compose-go seam: only internal/engine may import it
          MOD=$(go list -m)
          go list -f '{{.ImportPath}} {{join .Imports " "}}' ./... \
            | grep -v "^$MOD/internal/engine " | grep 'compose-spec/compose-go' \
            && { echo "compose-go leaked"; exit 1; } || echo "seam OK"
      - uses: golangci/golangci-lint-action@v6
  test:
    strategy: { matrix: { os: [ubuntu-latest, macos-latest] } }
    steps:
      - run: go build ./...
      - run: go test ./...
  acceptance:
    steps:
      - run: docker compose version
      - run: go test ./test/... -v            # docker-gated acceptance
      - env: { SMOKE_SKIP_DOCKER: "1" }        # and a no-docker job
        run: go test ./test/... -v
```

In your own project, the useful gate is `cenvkit validate` (or `validate --all`).
Because `env-debug` is daemon-free, you can also assert specific resolutions — and
catch env_file gaps — without Docker:

```sh
test "$(cenvkit env-debug --value --var SITE_URL)" = "example.com"
# Fail CI if a var your YAML references is only in a service env_file (a gap):
cenvkit env-debug --trace --var APP_PORT --json | grep -q '"gap": *true' \
  && { echo "APP_PORT is env_file-only — promote it to the chain"; exit 1; } || true
```

---

## 9. Behavior contracts (what you can rely on)

- **`env_file:` is runtime-only.** A `${VAR}` defined only in a service `env_file:`
  is **not** resolved at the run (native `:-default` fallback); the container still
  gets the var. `env-debug` flags the gap. Put a var in the **Layer-1 chain** to
  feed `${VAR}` interpolation.
- **Run path = Layer 1 only.** `cenvkit env-files` / `cenvkit compose` set
  `COMPOSE_ENV_FILES` to the Layer-1 chain only; no service `env_file:` path is
  ever included.
- **Missing `env_file:` (D1).** A missing *required* `env_file:` is lenient at
  enumeration (so `env-debug` never aborts) and **upstream-fatal at the real
  `docker compose` run** (which re-enforces `required:`). cenvkit never reimplements
  upstream's `required:` semantics.
- **Variable precedence.** `docker compose` owns precedence — last-wins over
  `COMPOSE_ENV_FILES`. cenvkit controls only the Layer-1 **file order**: chain order
  with `.secrets.env` last. Since `env_file:` is no longer in `COMPOSE_ENV_FILES`, a
  service `env_file:` can no longer override a chain/secret var at interpolation
  time. (At runtime, inline `environment:` still overrides `env_file:` within a
  service — native.)
- **`COMPOSE_FILE` overlays.** Selectors like
  `docker-compose.yml:docker-compose.${COMPOSE_ENV}.yml` work: cenvkit interpolates
  `${COMPOSE_ENV}`/`${ENV}` and splits on `COMPOSE_PATH_SEPARATOR` (else the OS
  path-list separator — never `,`).
- **`COMPOSE_DEPTH`** is **accepted-but-ignored** — the include-graph makes the old
  depth-bounded glob discovery obsolete; the var is tolerated (no error).
- **No over-discovery.** A compose file not in the `include:` graph / `COMPOSE_FILE`
  is never loaded; `.git` presence is irrelevant to discovery.
- **Determinism.** The resolved file list, the `--files` groups, and the JSON output
  are deterministically ordered (by service name, then declared order), so they are
  stable across runs and safe to diff/snapshot.
- **Debugger fidelity.** `env-debug` never reports a resolution the real run won't
  produce — `--trace`/`--effective` resolve against the same Layer-1 interpolation
  context the run uses.

---

## 10. Troubleshooting

**`${VAR}` resolves to its `:-default` fallback.**
Run `cenvkit env-debug --trace --var VAR`. If it shows a **gap** (`NOT in the
Layer-1 chain`), the value lives only in a service `env_file:` — that's runtime-only
and never feeds interpolation. Fix: add `VAR` to the Layer-1 chain (e.g. `.env`),
or stop referencing it as `${VAR}` in YAML.

**A container value isn't what I set in the env_file.**
`cenvkit env-debug --effective --service S` shows the final container env with the
source of each key. Remember inline `environment:` overrides `env_file:`, and inline
`${VAR}` interpolates against the chain (so an inline `"${X:-0}"` can override an
env_file's `X` with the fallback).

**`cenvkit env-files` doesn't list my service `env_file:`.**
By design — `env-files` is Layer-1 (`COMPOSE_ENV_FILES`) only. Service env_files are
runtime-only; see them via `cenvkit env-debug --files` (the runtime-only group).

**My secret got overridden.**
`.secrets.env` is last *within the Layer-1 chain* (it wins there). A service
`env_file:` can no longer affect interpolation, so it can't clobber a secret at
`${VAR}` time. If a value is still wrong, `env-debug --trace --var VAR` shows the
chain winner.

**`cenvkit compose` / `validate` fails with "Cannot connect to the Docker daemon".**
Those commands need `docker compose`. `env-debug` does not — use it for inspection
without Docker.

**`required` env_file error at `cenvkit compose up`, but `env-files` was fine.**
That's D1 by design: enumeration is lenient (skips a missing file) but the real
`docker compose` run enforces `required:`. Create the file or set `required: false`.

**`cenvkit init` didn't overwrite my `.env`.**
By design — `init` never clobbers an existing file. Delete it first for a fresh seed.

**A stray `compose.yaml` isn't being picked up in a monorepo.**
cenvkit follows the `include:` graph, not a filename glob. Add the subproject to the
root `include:` (or `COMPOSE_FILE`).

---

For the one-page reference, see [`cenvkit.md`](cenvkit.md).
