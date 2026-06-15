# Monorepo — root orchestrates subprojects

compose-envkit already handles subproject **isolation** (each subproject runs on
its own — see [integration → root vs isolated-subproject](integration.md#root-vs-isolated-subproject-setup)).
This guide covers the other half: the **root-orchestrates-subprojects** topology,
where a root compose `include:`s the subprojects into one unified stack and the
kit makes their per-subproject `env_file:` values resolve **from the root** too.

Runnable blueprint for everything below: [`examples/monorepo/`](../examples/monorepo/).

---

## The topology

```
monorepo/
├── docker-compose.yml      # ROOT: include:s web/ + api/, adds the shared network
├── .docker-env-chain       # root Layer-1 chain (.env → .${ENV}.env → .secrets.env)
├── .env                    # root non-secret defaults (from example.env)
├── Makefile                # include scripts/compose.mk + web-*/api-* delegation
├── docker                  # the shim (install.sh .)         ← root operates the unified stack
├── scripts/                # vendored engine (install.sh .)
├── web/
│   ├── docker-compose.yml  # service `web`, env_file: [./.web.env], "${WEB_PORT:-0}:80"
│   ├── .web.env            # WEB_PORT=18080  ← defined ONLY here
│   └── Makefile            # standalone: include ../scripts/compose.mk
└── api/
    ├── docker-compose.yml  # service `api`, env_file: [./.api.env], "${API_PORT:-0}:80"
    ├── .api.env            # API_PORT=19090  ← defined ONLY here
    └── Makefile            # standalone: include ../scripts/compose.mk
```

The root `docker-compose.yml` is the *base* file. It pulls each subproject in
with `include:` and adds only what is cross-cutting — a shared network and any
cross-service `depends_on`:

```yaml
# monorepo/docker-compose.yml
include:
  - path: ./web/docker-compose.yml
  - path: ./api/docker-compose.yml

services:
  web: { networks: [shared] }
  api:
    networks: [shared]
    depends_on:
      web: { condition: service_started }

networks:
  shared: { driver: bridge }
```

Each subproject compose file stays **self-contained**: it declares its own
service and its own `env_file:`, and it works standalone (run from its own dir)
*or* folded into the root via the `include:`. Subprojects don't reference each
other — cross-service wiring is root-only.

> `include:` needs **Docker Compose ≥ 2.24** (same floor as the kit's
> `env_file: required:` support).

---

## Cross-subproject Layer-2 — the thing the kit adds

The problem the kit solves at the single-project level
([concepts → the gap](concepts.md#the-gap-native-compose-doesnt-close)) gets
*worse* across an `include:` boundary. A subproject port lives in its own service
`env_file:`:

```yaml
# web/docker-compose.yml
services:
  web:
    env_file: [./.web.env]          # .web.env contains WEB_PORT=18080
    ports: ["${WEB_PORT:-0}:80"]    # interpolated BEFORE .web.env is read
```

Run native compose **from the root** and both subproject ports fall back:

```sh
$ docker compose config | grep -E 'WEB_PORT|API_PORT'
      WEB_PORT: "0"     # :-0 fallback — native compose never read web/.web.env
      API_PORT: "0"     # …nor api/.api.env, behind the include
```

`./docker` closes the gap by **discovering each subproject's `env_file:` from the
root** and folding them into `COMPOSE_ENV_FILES` (Layer 2). Now the ports resolve
from the root, exactly as if you ran each subproject standalone:

```sh
$ ./docker compose config | grep -E 'WEB_PORT|API_PORT|published'
      WEB_PORT: "18080"
      API_PORT: "19090"
        published: "18080"
        published: "19090"

$ ./docker env-files
  …/monorepo/.env
  …/monorepo/web/.web.env       ← discovered across the include
  …/monorepo/api/.api.env       ← discovered across the include
```

### How the discovery reaches across the include — `COMPOSE_DEPTH`

The engine (`lib/compose-env.sh`) finds compose files to feed the `env_file:`
parser with a depth-limited search:

```sh
find "$PROJECT_DIR" -maxdepth "$COMPOSE_DEPTH" -name 'docker-compose*.yml'
```

`COMPOSE_DEPTH` defaults to **3**. From the root, that depth is what lets the
search descend into `web/` and `api/` and pick up *their* `docker-compose.yml`,
so the parser extracts `web/.web.env` and `api/.api.env`. Each discovered
`env_file:` path is taken **relative to the directory of the compose file that
declared it** (the compose spec's rule), normalized to absolute, deduped against
Layer 1, and appended — so a subproject's vars resolve against that subproject's
own env file even when assembled from the root.

> **The depth knob is the whole reason cross-subproject discovery works.** Depth
> 3 reaches `<root>/<sub>/docker-compose.yml` (subproject at one level under the
> root). If your subprojects nest deeper — `<root>/services/<sub>/compose.yml`,
> or a subproject that itself has sub-subprojects — bump it:
>
> ```sh
> COMPOSE_DEPTH=4 ./docker compose config
> # or pin it per-project in the Makefile, before the include:
> #   export COMPOSE_DEPTH := 4
> ```
>
> Conversely, a very deep tree with many unrelated `docker-compose*.yml` files
> can be *narrowed* with a smaller depth so the parser doesn't pull in env files
> from compose stacks you never `include:`. The default 3 fits the common
> one-level-of-subprojects monorepo.

---

## The env-chain, layered for a monorepo

Two layers feed `COMPOSE_ENV_FILES`, last-wins, exactly as in the single-project
case ([concepts](concepts.md#the-two-layer-env-chain)) — the monorepo just spans
directories:

- **Layer 1 — the root project chain** (`.docker-env-chain`): `.env` →
  `.${ENV}.env` → `.secrets.env`, all at the **root**. These hold values the
  *whole stack* shares (`COMPOSE_PROJECT_NAME`, `SITE_URL`, the public URL) and
  drive `${VAR}` interpolation in the root + included YAML.
- **Layer 2 — every subproject's service `env_file:`**: `web/.web.env`,
  `api/.api.env`, auto-discovered to `COMPOSE_DEPTH`. These hold values a
  subproject owns (`WEB_PORT`, `API_PORT`) so the subproject stays runnable on
  its own.

**Placement rule of thumb:**

| The value is… | Put it in… | Why |
|---|---|---|
| shared by the whole stack, referenced as `${VAR}` in YAML | root `.env` / `.${ENV}.env` (Layer 1) | natively interpolated; no Layer-2 needed |
| owned by ONE subproject, also referenced in *its* YAML | that subproject's service `env_file:` (Layer 2) | subproject stays self-contained + standalone-runnable; kit folds it in from the root |
| a secret shared by the whole stack | root `.secrets.env` (last in Layer 1) | wins, gitignored |
| a secret owned by ONE subproject | that subproject's own gitignored env file, via its `env_file:` | discovered as Layer-2 for that subproject only |

---

## Root Makefile delegation (`make -C sub`)

The root Makefile drives the **unified** stack via `$(DC)` *and* delegates to
each subproject's own Makefile. Define the delegators **before** the include:

```make
# monorepo/Makefile
SUB_WEB = $(MAKE) --no-print-directory -C web
SUB_API = $(MAKE) --no-print-directory -C api

include scripts/compose.mk      # DC / DC_PROD / validate / help / env-debug*

##@ Unified — whole stack from root
up:   ; $(DC) up -d
down: ; $(DC) down --remove-orphans

##@ Delegation — a subproject's OWN Makefile, in isolation
web-build: ; $(SUB_WEB) build
api-build: ; $(SUB_API) build
```

`make up` operates the whole stack (root `include:` graph). `make web-build`
forwards to `web/`'s standalone Makefile via `$(MAKE) -C web`, which builds the
subproject in isolation. The subproject Makefiles ride on the root's vendored
engine with `include ../scripts/compose.mk` — the `.mk` resolves its script
paths relative to its own location, so the include works from the root and from
a subproject alike.

---

## Two ways to run — unified vs isolated

**Unified, from the root** (one stack):

```sh
sh /path/to/compose-envkit/install.sh .   # vendor scripts/ + ./docker at the root
cp example.env .env
./docker compose config       # all subproject ports resolve (cross-subproject Layer-2)
make up                       # whole stack
make validate                 # docker compose config -q
```

**Isolated, per subproject** (each independent) — pick one of the kit's two
subproject options ([integration](integration.md#root-vs-isolated-subproject-setup)):

```sh
# Option A — ride on the parent: drop a ./docker shim into the subproject; it
# walks up to the root's scripts/. Full Layer-2, zero extra files.
cp docker web/docker
cd web && ./docker compose config     # resolves web/'s OWN WEB_PORT, root-independent

# Option B — self-contained (cloneable standalone): give it its own scripts/.
sh /path/to/compose-envkit/install.sh web
```

Each `./docker` sets `PROJECT_DIR` to its own directory, so the root run and an
isolated run resolve **independent** chains and `env_file:` paths. Running web/
on its own never sees `api/`'s env, and vice-versa.

---

## Gotchas

- **Depth.** Cross-subproject discovery only reaches subprojects *within*
  `COMPOSE_DEPTH` (default 3) of the root. Subprojects nested deeper need
  `COMPOSE_DEPTH=N` bumped (see above). Symptom: a subproject port still shows
  the `:-0` fallback from the root but resolves correctly when run standalone →
  the root search isn't reaching that compose file; raise the depth.

- **Service-name collisions across subprojects.** `include:` merges services by
  name into one project. Two subprojects each defining a service called `web`
  collide (compose deep-merges them — usually not what you want). Keep service
  names unique across subprojects, or rename in the root with an override. The
  same applies to **networks** and **volumes** — the root owns the shared ones
  (here, the `shared` network); subprojects shouldn't redeclare them.

- **`env_file:` var-name collisions.** Layer-2 folds *all* discovered subproject
  env files into one `COMPOSE_ENV_FILES`, last-wins. If `web/.web.env` and
  `api/.api.env` both define `PORT`, the later-discovered one wins for *root*
  interpolation — a silent cross-talk. Prefix subproject-owned vars
  (`WEB_PORT` / `API_PORT`, not a bare `PORT`) so they never alias. Each
  subproject run *standalone* is unaffected (it only sees its own env file).

- **Secrets per subproject.** Keep a subproject's secrets in **its own**
  gitignored env file (referenced by its `env_file:`), not in the root
  `.secrets.env`. That keeps the secret scoped to the one subproject and present
  whether you run unified or isolated. Don't bake a secret into a `${...}`-built
  URL — same read-time gotcha as the single-project case
  ([concepts → Gotcha 2](concepts.md#gotcha-2----secrets-and--built-urls)).

- **Always go through `./docker`.** From the root, raw `docker compose` silently
  reintroduces the gap for *every* subproject `env_file:` at once. Use
  `./docker compose` (and `$(DC)` in the Makefile) so the cross-subproject
  Layer-2 fires.

---

## See also

- [`examples/monorepo/`](../examples/monorepo/) — the runnable blueprint this
  guide describes, with a README covering both run modes.
- [`docs/concepts.md`](concepts.md) — the two-layer chain and the
  `env_file:`→interpolation gap (single-project foundation).
- [`docs/integration.md`](integration.md#root-vs-isolated-subproject-setup) —
  the two subproject options (ride-on-parent vs self-contained) in full.
- `test/smoke-monorepo.sh` — proves cross-subproject Layer-2 end-to-end.
