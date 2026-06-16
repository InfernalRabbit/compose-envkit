# cenvkit — User Guide

A complete, end-to-end guide to `cenvkit`: what it solves, how to install it, how
to set up a project, every command, monorepos, the provenance debugger, CI, the
behavior contracts you can rely on, and troubleshooting.

For the terse reference, see [`cenvkit.md`](cenvkit.md). For the design rationale,
see [`superpowers/specs/`](superpowers/specs/).

---

## 1. What it is, and the gap it closes

`cenvkit` is a small Go CLI built on **Docker's own compose loader**
(`github.com/compose-spec/compose-go/v2`). It assembles `COMPOSE_ENV_FILES` from
the real, include-aware, interpolated compose model and then `exec`s
`docker compose`.

**The gap.** Docker Compose keeps two things deliberately separate:

| Layer | Populates | Visible to compose-time `${VAR}` in the YAML? |
|---|---|---|
| Project env (`--env-file` / `COMPOSE_ENV_FILES`) | the interpolation context | **yes** |
| A service's `env_file:` | the container's runtime env | **no** |

So if `APP_PORT` lives only in a service's `env_file:` and your YAML says
`ports: "${APP_PORT:-3000}:80"`, compose interpolates `${APP_PORT}` **before** it
reads that `env_file:` — you silently get the `:-3000` fallback. (Upstream
[docker/compose#3435](https://github.com/docker/compose/issues/3435), open since
2016.)

`cenvkit` enumerates each active service's resolved `env_file:` paths from the real
compose model and folds them into `COMPOSE_ENV_FILES` **in addition** to your
project chain. Now the same files that configure the container also feed
compose-time interpolation — last-wins.

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
$EDITOR .env .secrets.env

# 3. See the resolved chain (one path per line, in load order).
cenvkit env-files

# 4. Render the compose config — interpolation now includes the env_file: layer.
cenvkit compose config

# 5. Anything you'd run with `docker compose`, run with `cenvkit compose`.
cenvkit compose up -d
cenvkit compose logs -f web

# 6. When a value surprises you, ask why.
cenvkit env-debug --trace --var APP_PORT
```

The golden rule: **in an integrated project, always go through `cenvkit compose`,
not raw `docker compose`** — otherwise the `env_file:` layer is invisible to
interpolation again.

---

## 4. The env-chain (Layer 1 + Layer 2)

`COMPOSE_ENV_FILES` is built from two layers, in order, **last-wins**:

### Layer 1 — the project chain

Listed in `.docker-env-chain` (one path template per line; `#` comments and blank
lines ignored). If that file is absent, the built-in default is:

```
.env             # non-secret defaults (committed via example.env)
.${COMPOSE_ENV}.env   # per-environment overlay: .dev.env / .prod.env / …
.secrets.env     # secrets, gitignored — listed LAST so it wins within Layer 1
```

A typical explicit chain with a per-machine layer:

```
.env
.${ENV}.env
.${HOSTNAME}.env
.secrets.env
```

**Tokens** substituted in each entry: `${ENV}` and `${COMPOSE_ENV}` →
the resolved env; `${HOST}` and `${HOSTNAME}` → the machine hostname. Both are
**sanitized** to `[A-Za-z0-9._-]` (a hostname with `|`/`&`/`,` cannot inject a
path or split the file list). Non-existent files are silently skipped.

**`COMPOSE_ENV` resolution:** shell `COMPOSE_ENV` > a `COMPOSE_ENV=` line in
`.env` > `"dev"`.

**`HOST`/`HOSTNAME`:** exported `HOSTNAME` > the `hostname` command.

### Layer 2 — service `env_file:` paths

Enumerated from the real, include-aware compose model: every **active** service's
resolved, absolute `env_file:` paths, de-duplicated, appended after Layer 1.
Because the model is include-aware, this reaches **across `include:`** in a
monorepo (see §6), and there is **no filename-glob over-discovery** — a compose
file not in the `include:` graph or `COMPOSE_FILE` is never loaded.

### The merged result

```
<Layer 1, chain order, .secrets.env last>, <Layer 2, deterministic order>
```

`cenvkit env-files` prints this exact list. Variable **precedence** is then
`docker compose`'s own last-wins over that list (see §9).

---

## 5. Commands

Global flag: `--project-dir <dir>` — the project directory (default: the current
directory). Everything resolves relative to it.

| Command | What it does |
|---|---|
| `cenvkit compose <args…>` | assemble the chain, set `COMPOSE_ENV_FILES`, `exec docker compose <args…>` |
| `cenvkit env-files` | print the resolved `COMPOSE_ENV_FILES`, one absolute path per line |
| `cenvkit env-debug […] [--json]` | inspect the chain with provenance (in-process, no daemon) — see §7 |
| `cenvkit validate [--all]` | `docker compose config -q`; `--all` validates dev AND prod |
| `cenvkit init` | seed `.X` from `example.X` (**no-clobber**), fan out one directory level |
| `cenvkit version` | print the version |

### `cenvkit compose`

The core. It assembles `COMPOSE_ENV_FILES` and execs `docker compose`, passing all
arguments through. Exit code is propagated.

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
/app/.secrets.env
/app/web/.web.env
/app/api/.api.env
```

Use it to confirm exactly which files (and in what order) feed interpolation.

### `cenvkit validate`

```sh
cenvkit validate          # validate the currently-resolved COMPOSE_ENV
cenvkit validate --all    # validate dev AND prod (re-resolves the chain per env)
```

Non-zero exit on an invalid config. `--all` re-resolves the Layer-1 chain for each
environment (so `.dev.env`/`.prod.env` tiers are picked up correctly), not just the
docker subprocess env.

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

cenvkit loads the **real, include-aware** compose model, so two patterns work
out of the box.

### Root orchestrates subprojects (unified stack)

A root `docker-compose.yml` that `include:`s each subproject:

```yaml
# docker-compose.yml
include:
  - ./web
  - ./api
  - ./services/reports
```

Run the whole stack from the root. Layer-2 enumeration reaches **across** the
`include:`, so a `${WEB_PORT}` declared only in `web/.web.env` resolves even when
you render from the root — where native `docker compose config` would land on the
`:-0` fallback:

```sh
$ cenvkit compose config | grep -E 'WEB_PORT|API_PORT'
# both resolve to their real per-subproject values
$ cenvkit env-files | grep -E '\.web\.env|\.api\.env'
/app/web/.web.env
/app/api/.api.env
```

### Isolated subproject

Run from inside a subproject (or point at it) and it resolves **its own** chain
and `env_file:` set, blind to siblings:

```sh
cd web && cenvkit compose config      # web/'s own WEB_PORT
cenvkit --project-dir web env-files   # same, without cd
```

### dev / prod from one knob

Drive overlays with a single `COMPOSE_ENV`. A `COMPOSE_FILE` selector like
`docker-compose.yml:docker-compose.${COMPOSE_ENV}.yml` is interpolated by cenvkit
(see §9), and per-environment chain tiers (`.dev.env`/`.prod.env`,
`.<svc>.${COMPOSE_ENV}.env`) are picked up:

```sh
cenvkit compose config                      # dev (default)
COMPOSE_ENV=prod cenvkit compose config     # prod overlay + .prod.env tiers
```

---

## 7. `env-debug` — provenance

`env-debug` answers *"why does this variable have this value, and where does it
take effect?"* — entirely **in-process (no Docker daemon)**. Add `--json` to any
mode for the structured `Report` (tooling/CI).

| Mode | Shows |
|---|---|
| `--chain` (default) | the Layer-1 chain files, in load order (secrets last) |
| `--files` | the full merged `COMPOSE_ENV_FILES` (Layer 1 + Layer 2) |
| `--trace --var V` | V's winning value, the file that set it, the files it shadowed, and where `${V}` took effect (service/field → resolved value) |
| `--effective [--service S]` | each service's effective env, with the source of every value (`env_file:` vs inline `environment:`) |
| `--value --var V` | V's winning value, one line (for scripts) |

### `--trace` — one variable, end to end

```sh
$ cenvkit env-debug --trace --var WEB_PORT
WEB_PORT=18080
  winner:     /app/web/.web.env (layer2)
  overridden: /app/.env (layer1)
  effect:     service web field ports[0] -> 18080:80
```

That tells you: the value is `18080`, the winner is `web/.web.env` (a Layer-2
service env_file), it shadowed an earlier `.env` definition, and `${WEB_PORT}` is
used in `web`'s `ports[0]`, resolving to `18080:80`.

### `--effective` — a service's full env, with sources

```sh
$ cenvkit env-debug --effective --service web
service web:
  TIER=staging        <- (inline environment:) (environment)
  WEB_ONLY=yes        <- /app/web/.web.env (env_file)
  WEB_PORT=18080      <- /app/web/.web.env (env_file)
```

Inline `environment:` overrides `env_file:` values; the source layer of each key
is shown.

### `--json` — the structured Report

```sh
cenvkit env-debug --trace --var WEB_PORT --json
```

```json
{
  "files": ["/app/.env", "/app/.secrets.env", "/app/web/.web.env"],
  "chain_files": ["/app/.env", "/app/.secrets.env"],
  "vars": {
    "WEB_PORT": {
      "name": "WEB_PORT", "value": "18080",
      "winner": {"file": "/app/web/.web.env", "layer": "layer2"},
      "overridden": [{"file": "/app/.env", "layer": "layer1"}],
      "effects": [{"service": "web", "field": "ports[0]", "resolved": "18080:80"}]
    }
  },
  "services": [ /* per --effective */ ]
}
```

`files` is the full merged list; `chain_files` is the Layer-1-only subset that
`--chain` shows. Effects are reported for **service fields only**
(`services.<name>.<field>`); a `${VAR}` in top-level `networks:`/`volumes:`/`x-*`
still appears in chain attribution but not in `effects`.

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

In your own project, the useful gate is simply `cenvkit validate` (or
`cenvkit validate --all`) — it fails the build on an invalid resolved config, with
no need to spin containers. Because `env-debug` is daemon-free, you can also assert
specific resolutions in CI without Docker, e.g.:

```sh
test "$(cenvkit env-debug --value --var APP_PORT)" = "8080"
```

---

## 9. Behavior contracts (what you can rely on)

- **Missing `env_file:` (D1).** A missing *required* `env_file:` is **lenient at
  chain assembly** (cenvkit skips it so the chain never aborts) and
  **upstream-fatal at the real `docker compose` run** (which re-enforces
  `required:`). cenvkit never reimplements upstream's `required:` semantics.
- **Variable precedence.** `docker compose` owns precedence — last-wins over
  `COMPOSE_ENV_FILES`. cenvkit only controls the **file order**: Layer-1 in chain
  order (`.secrets.env` last *within Layer 1*), then Layer-2. A Layer-2 service
  `env_file:` is emitted *after* Layer 1, so if it reuses a chain variable name it
  wins at load time. **Do not reuse secret variable names in service `env_files`.**
- **`env_file:` paths.** A service `env_file:` *path* may reference only
  Layer-1/chain vars (e.g. `env_file: ${SVC_DIR}/.env` where `SVC_DIR` is in
  `.env`). A path depending on a var defined only inside *another* service's
  Layer-2 env_file is unsupported (single-pass) — it is not silently mis-resolved.
- **`COMPOSE_FILE` overlays.** Selectors like
  `docker-compose.yml:docker-compose.${COMPOSE_ENV}.yml` work: cenvkit
  interpolates `${COMPOSE_ENV}`/`${ENV}` and splits on `COMPOSE_PATH_SEPARATOR`
  (else the OS path-list separator — never `,`).
- **`COMPOSE_DEPTH`** is **accepted-but-ignored** — the include-graph makes the
  old depth-bounded glob discovery obsolete. Setting it is tolerated (no error)
  for back-compat.
- **No over-discovery.** A compose file not in the `include:` graph / `COMPOSE_FILE`
  is never loaded; `.git` presence is irrelevant to discovery.
- **Determinism.** The resolved file list and JSON output are deterministically
  ordered (sorted by service name, then declared order), so they are stable across
  runs and safe to diff/snapshot.

---

## 10. Troubleshooting

**`${VAR}` resolves to its `:-default` fallback.**
Run `cenvkit env-files` — is the file that defines `VAR` in the list? If it's a
service `env_file:`, confirm you're invoking via `cenvkit compose`, not raw
`docker compose`. Then `cenvkit env-debug --trace --var VAR` to see the winner.

**A value is "wrong" / not what I set.**
`cenvkit env-debug --trace --var VAR` shows the winner file and what it shadowed.
Remember last-wins: a later file in the chain (or a Layer-2 service env_file) beats
an earlier one.

**My secret got overridden.**
A service `env_file:` (Layer 2) is emitted after Layer 1, so it overrides a
same-named secret at load time. Rename the service variable — don't reuse secret
names in service `env_files` (see §9).

**`cenvkit compose` / `validate` fails with "Cannot connect to the Docker daemon".**
Those commands need `docker compose`. `env-debug` does not — use it for inspection
without Docker.

**`required` env_file error at `cenvkit compose up`, but `env-files` was fine.**
That's D1 by design: assembly is lenient (skips a missing file) but the real
`docker compose` run enforces `required:`. Create the file or set `required: false`.

**`cenvkit init` didn't overwrite my `.env`.**
By design — `init` never clobbers an existing file. Delete it first if you want a
fresh seed.

**A stray `compose.yaml` isn't being picked up in a monorepo.**
cenvkit follows the `include:` graph, not a filename glob. Add the subproject to
the root `include:` (or `COMPOSE_FILE`).

---

For the one-page reference, see [`cenvkit.md`](cenvkit.md).
