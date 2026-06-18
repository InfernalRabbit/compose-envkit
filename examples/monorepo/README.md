# Monorepo blueprint — root orchestrates subprojects

A complete, runnable example of the **root-`include:`-s-subprojects** topology:
a root compose that pulls in two subprojects (`web/`, `api/`) via `include:`,
plus a shared network. It demonstrates the gap that native compose leaves open
and how `cenvkit env-debug` surfaces it: `${WEB_PORT}` lives only in
`web/.web.env` (a service `env_file:`) — that file is **runtime-only**, so the
YAML interpolation falls back to `:-0`. `env-debug` tells you exactly what is
happening and what to fix.

The blueprint is driven entirely by **`cenvkit`** (the Go CLI) — there are no
Makefiles and no vendored wrapper scripts.

```
examples/monorepo/
├── docker-compose.yml         # root: include:s web/ api/ reports/ + a shared network (for the tools service)
├── docker-compose.dev.yml     # dev overlay  ┐ selected by COMPOSE_FILE's
├── docker-compose.prod.yml    # prod overlay ┘ ${CENVKIT_ENV} token
├── .cenvkit.envchain          # root Layer-1 chain (.env → .${ENV}.env → .${HOSTNAME}.env → .secrets.env)
├── example.env                # root non-secret defaults (cenvkit init → .env)
├── example.dev.env            # root dev tier (→ .dev.env);  IS_DEV=true
├── example.prod.env           # root prod tier (→ .prod.env); IS_DEV=false
├── web/
│   ├── docker-compose.yml     # service `web`, env_file: [./.web.env, ./.web.${CENVKIT_ENV}.env]
│   ├── .web.env               # WEB_PORT=18080  (defined ONLY here, runtime-only)
│   └── .web.dev.env / .web.prod.env   # per-service tier (WEB_DEBUG), by ${CENVKIT_ENV}
├── api/
│   ├── docker-compose.yml     # service `api`, env_file: [./.api.env], "${API_PORT:-0}:80"
│   └── .api.env               # API_PORT=19090  (defined ONLY here, runtime-only)
└── services/reports/          # nested deeper (legacy services/<svc>/ shape)
    ├── docker-compose.yml     # service `reports`, env_file: [./.reports.env]
    └── .reports.env           # REPORTS_PORT=15151  (defined ONLY here, runtime-only)
```

> This is a **source blueprint**. Real `.env` files are seeded from the
> committed `example.*` templates with `cenvkit init` (no-clobber) — that is
> exactly what `test/smoke-monorepo.sh` does.

---

## The two layers (Layer 1 vs service env_file:)

| Layer | Populates | Feeds `${VAR}` interpolation? |
|---|---|---|
| Layer-1 chain (`COMPOSE_ENV_FILES`, `.cenvkit.envchain`) | root `.env`, `.dev.env`, `.secrets.env`, … | **yes** |
| Service `env_file:` (`web/.web.env`, `api/.api.env`, …) | each container's runtime env | **no** |

`WEB_PORT=18080` lives only in `web/.web.env` — a service `env_file:`. At
compose-time interpolation, `${WEB_PORT}` is not in the Layer-1 chain, so it
falls back to `:-0`. The container still receives `WEB_PORT=18080` at runtime
(via the service `env_file:`). `cenvkit env-debug` surfaces exactly this.

---

## Run it

### A. Unified, from the root

```sh
cd examples/monorepo

# 1. Seed the real env files from the example.* templates (no-clobber).
cenvkit init                 # → .env, .dev.env, .prod.env (never clobbers)

# 2. See the Layer-1 chain (these are the files that feed interpolation).
cenvkit env-files
#   …/examples/monorepo/.env
#   …/examples/monorepo/.dev.env

# 3. See BOTH groups — interpolation chain AND runtime-only service env_files:
cenvkit env-debug --files
#   interpolation (COMPOSE_ENV_FILES):
#     …/examples/monorepo/.env
#     …/examples/monorepo/.dev.env
#   runtime-only (service env_file: — NOT interpolated, container env only):
#     api:
#       …/examples/monorepo/api/.api.env
#     reports:
#       …/examples/monorepo/services/reports/.reports.env
#     web:
#       …/examples/monorepo/web/.web.env
#       …/examples/monorepo/web/.web.dev.env

# 4. Render the unified config. ${WEB_PORT} falls back to 0 (Layer-1 only).
cenvkit compose config | grep -E 'WEB_PORT|API_PORT|published'
#   WEB_PORT: "0"          ← :-0 fallback; WEB_PORT is not in the Layer-1 chain
#   API_PORT: "0"          ← same
#     published: "0"

# 5. Validate (docker compose config -q):
cenvkit validate
```

Native compose from the root behaves identically on the fallback — the gap is
Docker's native behavior, not something cenvkit adds:

```sh
docker compose config | grep -E 'WEB_PORT|API_PORT|published'
#   WEB_PORT: "0"        ← same :-0 fallback
#   API_PORT: "0"
```

### B. Isolated, per subproject (each runs on its own)

A subproject runs independently — point `cenvkit` at its directory. Everything
resolves relative to that project directory:

```sh
# From the subproject directory:
cd examples/monorepo/web
cenvkit env-files
#   …/web/.env    (if it exists after init in the subproject)

# …or without changing directory, with --project-dir:
cenvkit --project-dir examples/monorepo/web env-files
```

Running from the root (A) and running isolated (B) are fully independent: each
invocation resolves *its own* chain and `env_file:` paths against its project
directory.

---

## The gap-detector: env-debug

`cenvkit env-debug` is daemon-free — no Docker needed.

```sh
# Trace WEB_PORT: see the gap (runtime-only, falls back at interpolation time).
cenvkit env-debug --trace --var WEB_PORT
# WEB_PORT
#   interpolation: NOT in the Layer-1 chain -> ${WEB_PORT} falls back at run time
#   runtime:       …/web/.web.env -> WEB_PORT=18080  (service `web` container env only)
#   ⚠ gap: ${WEB_PORT} used in service web environment[1] resolves to "0" at the run, NOT the env_file value (defined only in a service env_file).
#   ⚠ gap: ${WEB_PORT} used in service web ports[0] resolves to "0:80" at the run, NOT the env_file value (defined only in a service env_file).
#   fix:   add WEB_PORT to the Layer-1 chain (e.g. .env), or use it runtime-only.

# Show the final container env for the web service (with sources):
cenvkit env-debug --effective --service web
# service web:
#   IS_DEV=true	<- (inline environment:) (environment)
#   STACK_TIER=dev	<- (inline environment:) (environment)
#   WEB_DEBUG=true	<- …/web/.web.dev.env (env_file)
#   WEB_PORT=0	<- (inline environment:) (environment)
```

The `--effective` output shows the **truth about the container**: `WEB_PORT=0`
comes from the inline `environment: WEB_PORT: "${WEB_PORT:-0}"` — interpolated
against the Layer-1 chain, which does not have `WEB_PORT`, so the `:-0` fallback
wins. That overrides the `18080` in `web/.web.env` (inline `environment:` always
wins over `env_file:`).

**To make `${WEB_PORT}` interpolate** (e.g. to publish the real port): add
`WEB_PORT=18080` to the Layer-1 chain (root `.env` or `web/.env`). Then both
`env-files` and `env-debug --trace` will show it as a chain variable with no gap.

---

## dev / prod and per-machine overrides

One `CENVKIT_ENV` knob (shell > `.env` > `dev`) drives the whole split:

```sh
cenvkit init                 # seeds .dev.env / .prod.env from the templates

CENVKIT_ENV=prod cenvkit compose config | grep STACK_TIER
#   STACK_TIER: prod      ← docker-compose.prod.yml overlay (COMPOSE_FILE selector)
```

Per-machine tweaks (log level, local paths) go in an optional `.${HOSTNAME}.env`
that the chain already lists — it beats the shared `.env` but stays **below**
`.secrets.env`. Never put a secret in a host file.

Optional services hide behind a profile (`COMPOSE_PROFILES` is forwarded to
compose unchanged). The blueprint ships a `tools` service off by default:

```sh
cenvkit compose config --services                       # web, api
COMPOSE_PROFILES=tools cenvkit compose config --services # web, api, tools
```

---

## Verify

`test/smoke-monorepo.sh` runs this whole blueprint end-to-end against the
`cenvkit` binary — confirming that `env-files` returns Layer-1 only, that
`${WEB_PORT}` falls back to `0` (runtime-only gap), and that `env-debug --trace`
reports the gap correctly:

```sh
sh test/smoke-monorepo.sh
```
