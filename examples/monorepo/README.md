# Monorepo blueprint — root orchestrates subprojects

A complete, runnable example of the **root-`include:`-s-subprojects** topology:
a root compose that pulls in two subprojects (`web/`, `api/`) via `include:`,
plus a shared network and root Makefile delegation. It demonstrates the one
thing native compose can't do across an `include:` boundary — **cross-subproject
Layer-2**: a `${WEB_PORT}` declared only in `web/.web.env` (and `${API_PORT}` in
`api/.api.env`) resolving correctly even when you render the whole stack *from
the root*.

For the full guide and the gotchas, see [`docs/monorepo.md`](../../docs/monorepo.md).

```
examples/monorepo/
├── docker-compose.yml         # root: include:s web/ + api/, adds the shared network
├── docker-compose.dev.yml     # dev overlay  ┐ selected by COMPOSE_FILE's
├── docker-compose.prod.yml    # prod overlay ┘ ${COMPOSE_ENV} token
├── .docker-env-chain          # root Layer-1 chain (.env → .${ENV}.env → .${HOSTNAME}.env → .secrets.env)
├── example.env                # root non-secret defaults (cp → .env)
├── example.dev.env            # root dev tier (cp → .dev.env);  IS_DEV=true
├── example.prod.env           # root prod tier (cp → .prod.env); IS_DEV=false
├── Makefile                   # include scripts/compose.mk + web-*/api-* delegation
├── web/
│   ├── docker-compose.yml     # service `web`, env_file: [./.web.env, ./.web.${COMPOSE_ENV}.env]
│   ├── .web.env               # WEB_PORT=18080  (defined ONLY here, env-agnostic)
│   ├── .web.dev.env / .web.prod.env   # per-service tier (WEB_DEBUG), by ${COMPOSE_ENV}
│   └── Makefile               # standalone: include ../scripts/compose.mk
└── api/
    ├── docker-compose.yml     # service `api`, env_file: [./.api.env], "${API_PORT:-0}:80"
    ├── .api.env               # API_PORT=19090  (defined ONLY here)
    └── Makefile               # standalone: include ../scripts/compose.mk
```

> This is a **source blueprint** — it does NOT ship a copy of `scripts/` or a
> `./docker` shim. You wire those in with `install.sh` (below); that is exactly
> what `test/smoke-monorepo.sh` does.

---

## Run it BOTH ways

### A. Unified, from the root (one stack, cross-subproject Layer-2)

```sh
cd examples/monorepo

# 1. Vendor the kit at the ROOT (drops ./docker + scripts/, generates nothing
#    that would clobber the committed .docker-env-chain / example.env).
sh /path/to/compose-envkit/install.sh .

# 2. Real root env (non-secret) from the template.
cp example.env .env

# 3. Render the unified config. The root ./docker discovers BOTH subprojects'
#    env_file: paths (Layer-2, to COMPOSE_DEPTH=3) and folds them in, so the
#    subproject ports resolve from the root.
./docker compose config | grep -E 'WEB_PORT|API_PORT|published'
#   WEB_PORT: "18080"
#   API_PORT: "19090"
#     published: "18080"
#     published: "19090"

# Prove the env-files chain lists both subproject env files:
./docker env-files
#   …/examples/monorepo/.env
#   …/examples/monorepo/web/.web.env      ← discovered across the include
#   …/examples/monorepo/api/.api.env      ← discovered across the include

# Whole stack via make (root Makefile, after `echo 'include scripts/compose.mk'`
# is already in the shipped Makefile):
make config        # = ./docker compose config
make up            # bring the unified stack up
make validate      # docker compose config -q
```

Compare to **native** compose from the root — the gap the kit closes:

```sh
docker compose config | grep -E 'WEB_PORT|API_PORT|published'
#   WEB_PORT: "0"        ← :-0 fallback; native compose never read web/.web.env
#   API_PORT: "0"        ← …nor api/.api.env, because they sit behind the include
```

### B. Isolated, per subproject (each runs on its own)

A subproject runs independently of the root stack. Per the kit's two subproject
options ([`docs/integration.md`](../../docs/integration.md#root-vs-isolated-subproject-setup)):

```sh
# Option A — ride on the parent: drop a ./docker shim into the subproject; it
# walks up to the root's scripts/. Zero extra files, full Layer-2.
cp examples/monorepo/docker examples/monorepo/web/docker   # after install.sh . above
cd examples/monorepo/web
./docker compose config | grep -E 'WEB_PORT|published'
#   WEB_PORT: "18080"      ← resolves its OWN .web.env, independent of the root

# Its standalone Makefile rides on ../scripts/ via `include ../scripts/compose.mk`:
make config        # web's own config
make up            # run web in isolation

# Option B — self-contained (cloneable on its own): give the subproject its own
# scripts/ copy so it works with no parent reachable.
sh /path/to/compose-envkit/install.sh examples/monorepo/web
cd examples/monorepo/web && ./docker compose config
```

Running from the root (A) and running isolated (B) are fully independent: each
`./docker` sets `PROJECT_DIR` to its own dir and resolves *its own* chain and
`env_file:` paths.

---

## dev / prod and per-machine overrides

One `COMPOSE_ENV` knob (shell > `.env` > `dev`) drives the whole split — see
[`docs/monorepo.md`](../../docs/monorepo.md#dev--prod--one-compose_env-knob) for
the full story:

```sh
cp example.dev.env .dev.env && cp example.prod.env .prod.env

COMPOSE_ENV=prod ./docker compose config | grep STACK_TIER
#   STACK_TIER: prod      ← docker-compose.prod.yml overlay (COMPOSE_FILE selector)

COMPOSE_ENV=prod ./docker env-files | grep -E '\.web\.|\.prod\.env'
#   …/web/.web.prod.env   ← per-service tier switched with the env
#   …/.prod.env           ← root tier (IS_DEV=false)
```

Per-machine tweaks (log level, local paths) go in an optional `.${HOSTNAME}.env`
that the chain already lists — it beats the shared `.env` but stays **below**
`.secrets.env`. Never put a secret in a host file. See
[Per-machine overrides](../../docs/monorepo.md#per-machine-overrides--hostnameenv).

Optional services hide behind a profile (passthrough — the shim forwards
`COMPOSE_PROFILES` to compose unchanged). The blueprint ships a `tools` service
off by default:

```sh
./docker compose config --services                       # web, api
COMPOSE_PROFILES=tools ./docker compose config --services # web, api, tools
```

---

## Verify

`test/smoke-monorepo.sh` runs this whole blueprint end-to-end (root resolves
both subproject ports via cross-subproject Layer-2, an isolated subproject
resolves its own port, and a native baseline shows the `:-0` fallback to prove
the win):

```sh
sh test/smoke-monorepo.sh
```
