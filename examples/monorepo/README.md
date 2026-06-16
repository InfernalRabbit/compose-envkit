# Monorepo blueprint — root orchestrates subprojects

A complete, runnable example of the **root-`include:`-s-subprojects** topology:
a root compose that pulls in two subprojects (`web/`, `api/`) via `include:`,
plus a shared network. It demonstrates the one thing native compose can't do
across an `include:` boundary — **cross-subproject Layer-2**: a `${WEB_PORT}`
declared only in `web/.web.env` (and `${API_PORT}` in `api/.api.env`) resolving
correctly even when you render the whole stack *from the root*.

The blueprint is driven entirely by **`cenvkit`** (the Go CLI) — there are no
Makefiles and no vendored wrapper scripts.

```
examples/monorepo/
├── docker-compose.yml         # root: include:s web/ + api/, adds the shared network
├── docker-compose.dev.yml     # dev overlay  ┐ selected by COMPOSE_FILE's
├── docker-compose.prod.yml    # prod overlay ┘ ${COMPOSE_ENV} token
├── .docker-env-chain          # root Layer-1 chain (.env → .${ENV}.env → .${HOSTNAME}.env → .secrets.env)
├── example.env                # root non-secret defaults (cenvkit init → .env)
├── example.dev.env            # root dev tier (→ .dev.env);  IS_DEV=true
├── example.prod.env           # root prod tier (→ .prod.env); IS_DEV=false
├── web/
│   ├── docker-compose.yml     # service `web`, env_file: [./.web.env, ./.web.${COMPOSE_ENV}.env]
│   ├── .web.env               # WEB_PORT=18080  (defined ONLY here, env-agnostic)
│   └── .web.dev.env / .web.prod.env   # per-service tier (WEB_DEBUG), by ${COMPOSE_ENV}
├── api/
│   ├── docker-compose.yml     # service `api`, env_file: [./.api.env], "${API_PORT:-0}:80"
│   └── .api.env               # API_PORT=19090  (defined ONLY here)
└── services/reports/          # nested deeper (legacy services/<svc>/ shape)
    ├── docker-compose.yml     # service `reports`, env_file: [./.reports.env]
    └── .reports.env           # REPORTS_PORT=15151  (reached via the include graph from root)
```

> This is a **source blueprint**. Real `.env` files are seeded from the
> committed `example.*` templates with `cenvkit init` (no-clobber) — that is
> exactly what `test/smoke-monorepo.sh` does.

---

## Run it BOTH ways

### A. Unified, from the root (one stack, cross-subproject Layer-2)

```sh
cd examples/monorepo

# 1. Seed the real env files from the example.* templates (no-clobber). cenvkit
#    init also fans out one directory level to seed each immediate subproject.
cenvkit init                 # → .env, .dev.env, .prod.env (never clobbers)

# 2. Render the unified config. cenvkit loads the real, include-aware compose
#    model, enumerates BOTH subprojects' env_file: paths (Layer-2) and folds
#    them in, so the subproject ports resolve from the root.
cenvkit compose config | grep -E 'WEB_PORT|API_PORT|published'
#   WEB_PORT: "18080"
#   API_PORT: "19090"
#     published: "18080"
#     published: "19090"

# Prove the env-files chain lists both subproject env files:
cenvkit env-files
#   …/examples/monorepo/.env
#   …/examples/monorepo/web/.web.env      ← enumerated across the include
#   …/examples/monorepo/api/.api.env      ← enumerated across the include

# Inspect the chain, in load order, and validate:
cenvkit env-debug --chain
cenvkit validate             # docker compose config -q
```

Compare to **native** compose from the root — the gap `cenvkit` closes:

```sh
docker compose config | grep -E 'WEB_PORT|API_PORT|published'
#   WEB_PORT: "0"        ← :-0 fallback; native compose never read web/.web.env
#   API_PORT: "0"        ← …nor api/.api.env, because they sit behind the include
```

### B. Isolated, per subproject (each runs on its own)

A subproject runs independently of the root stack — point `cenvkit` at the
subproject's directory (or `cd` into it). Everything resolves relative to that
project directory:

```sh
# From the subproject directory:
cd examples/monorepo/web
cenvkit compose config | grep -E 'WEB_PORT|published'
#   WEB_PORT: "18080"      ← resolves its OWN .web.env, independent of the root

# …or without changing directory, with --project-dir:
cenvkit --project-dir examples/monorepo/web compose config
cenvkit --project-dir examples/monorepo/web env-files
```

Running from the root (A) and running isolated (B) are fully independent: each
invocation resolves *its own* chain and `env_file:` paths against its project
directory.

---

## dev / prod and per-machine overrides

One `COMPOSE_ENV` knob (shell > `.env` > `dev`) drives the whole split:

```sh
cenvkit init                 # seeds .dev.env / .prod.env from the templates

COMPOSE_ENV=prod cenvkit compose config | grep STACK_TIER
#   STACK_TIER: prod      ← docker-compose.prod.yml overlay (COMPOSE_FILE selector)

COMPOSE_ENV=prod cenvkit env-files | grep -E '\.web\.|\.prod\.env'
#   …/web/.web.prod.env   ← per-service tier switched with the env
#   …/.prod.env           ← root tier (IS_DEV=false)
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
`cenvkit` binary (root resolves both subproject ports via cross-subproject
Layer-2, an isolated subproject resolves its own port, and a native baseline
shows the `:-0` fallback to prove the win):

```sh
sh test/smoke-monorepo.sh
```
