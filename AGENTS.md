# AGENTS.md — compose-envkit integration guide (for AI agents)

You are integrating **compose-envkit** into a repository. This file is the
machine-actionable contract: what the kit is, how to install it, what you must
never break, and how to verify the result. Read it fully before acting. All
paths are relative to the kit checkout unless stated otherwise.

## What this kit does (and why it exists)

`docker compose` interpolates `${VAR}` in a compose YAML using variables from
**`COMPOSE_ENV_FILES`** (or `.env`). It does **NOT** use a service's own
`env_file:` entries for that interpolation — those are only injected into the
container at runtime. So a line like:

```yaml
ports: ["${APP_PORT:-3000}:3000"]   # APP_PORT defined in the service's env_file
```

resolves `APP_PORT` to the `:-3000` fallback at compose time, even though the
value sits in an `env_file:` the service references. **Closing that gap is the
entire reason this kit exists.** The kit discovers every service's `env_file:`
paths and folds them into `COMPOSE_ENV_FILES`, so compose-time `${VAR}` refs
see those values.

**Do NOT "simplify" an integrated project by replacing `./docker compose` with
raw `docker compose`.** That silently reintroduces the gap and breaks any
interpolation that depends on a service `env_file:`.

## Architecture (five parts)

1. **Shim** — `bin/docker`, installed as `<project>/docker` (executable). A thin,
   self-locating POSIX front-end. It does NOT contain the logic; it locates the
   engine and `exec`s it, passing its own directory as `PROJECT_DIR`. Has an
   inline Layer-1-only fallback for standalone clones with no reachable engine.
2. **Engine (lib)** — `lib/compose-env.sh`. Builds `COMPOSE_ENV_FILES` from two
   layers, then dispatches to `docker` / `docker compose`. This is where the gap
   is closed.
3. **Parser (lib)** — `lib/parse-compose-env-files.sh`. Portable-awk YAML reader
   that extracts each service's `env_file:` paths (Layer 2). Single source of
   truth, shared by the engine and the debugger.
4. **Debugger (lib)** — `lib/env-debug.sh`. The whole debug flow. Modes:
   `--chain` (default), `--diff`, `--effective`, `--files`, `--trace --var NAME`,
   `--value --var NAME`. Filters: `--service NAME`, `--var NAME`.
5. **Make glue (mk)** — `mk/compose.mk` (vars `DC` / `DC_PROD` / `PLATFORM` +
   `validate` / `help`) which transitively includes `mk/env-debug.mk`
   (`env-debug*` targets). Plus `completions/` for tab-completion.

### The two layers (`COMPOSE_ENV_FILES`, last-wins)

- **Layer 1 — project chain.** Read from `<project>/.docker-env-chain` (one
  relative path per line; blank/`#` lines ignored; `${ENV}` / `${COMPOSE_ENV}`
  substituted; missing files silently skipped). Default order when no chain file
  exists: `.env` → `.${ENV}.env` → `.secrets.env`. These drive `${VAR}`
  interpolation in the YAML itself.
- **Layer 2 — container `env_file:`.** Auto-discovered from `docker-compose*.yml`
  (depth `COMPOSE_DEPTH`, default 3) by the parser, `${COMPOSE_ENV:-default}`
  substituted, normalized to absolute, **deduped against Layer 1**, appended.

`COMPOSE_ENV` resolution everywhere: shell `COMPOSE_ENV` > `<project>/.env`'s
`COMPOSE_ENV=` line > `dev`.

## File / layout conventions

The shim locates the engine by checking, **first-found-wins**:
`$SELF_DIR/scripts/compose-env.sh` then `$SELF_DIR/../scripts/compose-env.sh`.
That is why the install layout is fixed — honor it exactly:

```
<project>/docker                         # the shim (bin/docker)
<project>/scripts/compose-env.sh         # engine
<project>/scripts/parse-compose-env-files.sh
<project>/scripts/env-debug.sh
<project>/scripts/compose.mk             # → include scripts/compose.mk
<project>/scripts/env-debug.mk           # pulled in transitively by compose.mk
<project>/scripts/completions/{env-debug.bash,env-debug.zsh}
<project>/.docker-env-chain              # Layer-1 order (generated if absent)
<project>/example.env, example.prod.env, example.secrets.env   # templates
```

Note the **flattened** vendor layout: `lib/*.sh` and `mk/*.mk` all land
side-by-side under `scripts/`, so `include scripts/compose.mk` and the shim's
`scripts/compose-env.sh` lookup both resolve by plain name.

`.docker-env-chain` example (the generated default):

```
.env
.${ENV}.env
.secrets.env       # MUST be last — secrets win, and stay gitignored
```

Do **not** list container `env_file:` paths in `.docker-env-chain`; they are
auto-discovered. Doing so is redundant (the engine dedups) but signals a
misunderstanding.

## How to integrate into a fresh project

### Path A — installer (preferred)

From the kit checkout, with the target directory **already existing**:

```sh
sh install.sh /path/to/project      # default target is "." (cwd)
sh install.sh --dry-run /path/to/project   # print the plan, write nothing
sh install.sh --help
```

The installer is **idempotent** and never clobbers real env files:

- Vendors `lib/*.sh` (chmod +x) and `mk/*.mk` into `<project>/scripts/`.
- Vendors `completions/*` into `<project>/scripts/completions/`.
- Copies `bin/docker` → `<project>/docker` (chmod +x).
- Generates `<project>/.docker-env-chain` and `<project>/example.*` **only if
  absent**. It NEVER writes or touches `.env` / `.secrets.env`.
- Refuses to install into its own source dir.

Then finish the wiring (the installer prints these):

```sh
# 1. one line in the project Makefile (do this yourself — installer won't edit it):
echo 'include scripts/compose.mk' >> /path/to/project/Makefile

# 2. create real env files from templates (you fill the values):
cp example.env .env
cp example.secrets.env .secrets.env    # gitignored

# 3. ensure .gitignore ignores the runtime env files (see "Invariants" below)
```

### Path B — manual

1. `mkdir -p <project>/scripts/completions`
2. Copy `lib/*.sh` → `<project>/scripts/` and `chmod +x` them.
3. Copy `mk/compose.mk`, `mk/env-debug.mk` → `<project>/scripts/`.
4. Copy `completions/*` → `<project>/scripts/completions/`.
5. Copy `bin/docker` → `<project>/docker`; `chmod +x <project>/docker`.
6. Copy `templates/docker-env-chain` → `<project>/.docker-env-chain` (only if
   absent). Copy `templates/example.*` → `<project>/` (only if absent).
7. Add `include scripts/compose.mk` to the Makefile.
8. `cp example.env .env`; `cp example.secrets.env .secrets.env`; fill them in.
9. Add the env-file ignore rules to `.gitignore` (see Invariants).

### Subproject case (monorepo)

A subproject inside an already-installed repo needs its own `./docker`. Two
options:

- **Rely on the parent:** copy just `bin/docker` to `<subproject>/docker`. The
  shim walks up to the parent's `scripts/compose-env.sh` (`$SELF_DIR/../scripts`).
- **Self-contained (cloneable standalone):** run
  `sh install.sh <subproject-dir>` to vendor its own `scripts/` copy.

The shim self-locates either way. `PROJECT_DIR` is always the shim's own
directory, so each subproject resolves its chain and compose files against
itself.

## Usage after integration

```sh
./docker env-files            # print the resolved COMPOSE_ENV_FILES (one path/line)
./docker compose config       # compose, with both layers folded in
./docker compose up -d        # same — ALWAYS go through ./docker, not raw docker compose
./docker <anything-else>      # passthrough to `docker`

make validate                 # docker compose config -q for dev + prod
make help                     # grouped target list
make env-debug                # the chain, in load order, with origin tags
make env-debug-diff           # what each file adds (+) / overrides (~) / repeats (·)
make env-debug-effective      # final per-service values (via docker compose config)
make env-debug-files          # bare path list (for xargs/grep)
make env-debug-trace VAR=NAME # call stack for one variable
make env-debug SERVICE=web VAR=DB_HOST    # filters combine
make install-completions      # prints the source line for bash/zsh completion
```

`COMPOSE_ENV=prod ./docker compose …` (or `make` targets driven by `DC_PROD`)
selects the prod layer. Override `DC` / `DC_PROD` / `PLATFORM` in the project
Makefile **before** the `include scripts/compose.mk` line.

## Keep project-specific — do NOT hardcode into the kit

The kit ships **neutral**. Service names, hostnames, real domains, TLS/ingress
overlays, and overlay file lists belong in the **project's** `.env` /
`.prod.env` / Makefile, never in `mk/*.mk` or `lib/*.sh`. Wire overlays through
`COMPOSE_FILE`, e.g.
`COMPOSE_FILE=docker-compose.yml${OVERLAY:+:docker-compose.tls.yml}:docker-compose.${COMPOSE_ENV}.yml`.
Gotcha: `${FLAG:+…}` expands at env-parse time, so a flag you only set in
`.prod.env` must also be re-pinned in that file's `COMPOSE_FILE` (see
`templates/example.prod.env`).

## Hard invariants & gotchas

- **POSIX `sh` only** in shipped `*.sh` and `bin/docker`. No bashisms: no arrays,
  no `[[ ]]`, no `local` (unguarded), no `${var,,}`, no process substitution, no
  `echo -e`. Use `printf`. Portable awk only (no gawk extensions). No GNU
  `readlink -f` / `realpath` / `sed -i` — resolve paths with `CDPATH= cd -- … &&
  pwd`. (`completions/*.bash` is intentionally bash and is NOT run under sh.)
- **The env_file→interpolation gap is WHY the wrapper exists.** Never bypass
  `./docker` with raw `docker compose` in an integrated project.
- **Secrets are gitignored.** `.secrets.env` (and `.env`, `.*.env`) must be in
  `.gitignore`; only the `example.*` templates are committed. The shipped
  `.gitignore` shows the pattern:
  ```
  .env
  .*.env
  .secrets.env
  !example.env
  !example.*.env
  !example.secrets.env
  ```
  Never put a secret in a committed template or in any URL string built in
  `.env` (a `${REDIS_PASSWORD}` interpolated in `.env` resolves before
  `.secrets.env` loads → blank). Consume discrete `HOST`/`PORT`/`PASSWORD` vars.
- **`.secrets.env` must be last** in the chain so it wins.
- **`./docker` must stay executable** (`chmod +x`). `env-debug.sh` aborts if
  `./docker` is missing — it discovers the chain by calling the shim.
- **`env-debug --value`** sources ONLY the project chain (Layer 1) in a sh
  subshell under `set +u`; it does NOT source container `env_file:` paths (those
  hold bare compose-refs, unsafe to shell-source). Use it for scalar values in
  Make/scripts; use `--effective` / `--trace` for container-level resolution.
- **Layer-2 needs real `docker compose`.** `--effective`, `--trace`, `--diff`
  per-service, and the smoke acceptance check shell out to `docker compose
  config`. Without docker, only `--value` and Layer-1 inspection work.
- **Don't hand-edit vendored `scripts/` copies** — they are owned by the kit and
  the installer overwrites them on re-run. Patch the kit and re-vendor instead.

## How to verify an integration

Run, from the kit, the bundled tests (they need POSIX sh; the Layer-2 assertion
needs `docker compose`):

```sh
sh test/lint.sh     # sh -n + shellcheck (if present) on every shipped script
sh test/smoke.sh    # installs into a temp project, asserts Layer-2, runs every mode
```

`test/smoke.sh` is the canonical proof: it scaffolds a throwaway project whose
`web` service has `env_file: [./svc.env]` and a `${SVC_PORT:-0}` port, with
`SVC_PORT` defined ONLY in `svc.env`, installs the kit, and asserts
`./docker compose config` resolves the port to the real value (24680), not the
`:-0` fallback — proving Layer-2 fired. It also runs every `env-debug` mode,
`./docker env-files`, the `make env-debug*` targets, and repeats from a subdir.
(Set `SMOKE_SKIP_DOCKER=1` to downgrade the docker-dependent checks to skips.)

To verify a **real** target project after integrating:

```sh
cd /path/to/project
./docker env-files            # non-empty; includes Layer-1 .env AND discovered env_file: paths
./docker compose config       # succeeds; ${VAR}s from service env_file: are resolved
make validate                 # dev + prod compose config both OK
make env-debug                # chain prints with [.docker-env-chain] / [compose env_file: svc] tags
make env-debug-trace VAR=SOME_VAR   # shows definition → references → effective value
```

Green smoke + a non-empty `./docker env-files` listing both layers + a
successful `./docker compose config` with the expected interpolated value is a
passing integration.
