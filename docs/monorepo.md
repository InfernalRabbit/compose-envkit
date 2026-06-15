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
├── docker-compose.yml         # ROOT: include:s web/ + api/, adds the shared network
├── docker-compose.dev.yml     # dev overlay  ┐ selected by COMPOSE_FILE's
├── docker-compose.prod.yml    # prod overlay ┘ ${COMPOSE_ENV} token
├── .docker-env-chain          # root Layer-1 chain (.env → .${ENV}.env → .${HOSTNAME}.env → .secrets.env)
├── .env                       # root non-secret defaults (from example.env)
├── .dev.env / .prod.env       # root per-env tier (from example.{dev,prod}.env); carries IS_DEV
├── .${HOSTNAME}.env           # OPTIONAL per-machine override (not committed)
├── Makefile                   # include scripts/compose.mk + web-*/api-* delegation
├── docker                     # the shim (install.sh .)      ← root operates the unified stack
├── scripts/                   # vendored engine (install.sh .)
├── web/
│   ├── docker-compose.yml     # service `web`, env_file: [./.web.env, ./.web.${COMPOSE_ENV}.env]
│   ├── .web.env               # WEB_PORT=18080  ← defined ONLY here (env-agnostic)
│   ├── .web.dev.env / .web.prod.env  # per-service tier, selected by ${COMPOSE_ENV}
│   └── Makefile               # standalone: include ../scripts/compose.mk
├── api/
│   ├── docker-compose.yml     # service `api`, env_file: [./.api.env], "${API_PORT:-0}:80"
│   ├── .api.env               # API_PORT=19090  ← defined ONLY here
│   └── Makefile               # standalone: include ../scripts/compose.mk
└── services/
    └── reports/               # nested one level deeper (the legacy services/<svc>/ shape)
        ├── docker-compose.yml # service `reports`, env_file: [./.reports.env]
        └── .reports.env       # REPORTS_PORT=15151  ← reached from root at depth 3
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
  `.${ENV}.env` → `.${HOSTNAME}.env` (optional per-machine) → `.secrets.env`, all
  at the **root**. These hold values the *whole stack* shares
  (`COMPOSE_PROJECT_NAME`, `SITE_URL`, the public URL, `IS_DEV`) and drive
  `${VAR}` interpolation in the root + included YAML. See
  [Per-machine overrides](#per-machine-overrides--hostnameenv) and
  [dev / prod](#dev--prod--one-compose_env-knob) below.
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

## Per-machine overrides — `.${HOSTNAME}.env`

One dev box needs `trace` logging, another a different local path, a CI runner a
different image — without forking the shared `.env`. The root chain supports a
**per-machine layer** keyed on the hostname:

```
# .docker-env-chain
.env
.${ENV}.env
.${HOSTNAME}.env      # ← selected by the machine hostname; beats .env / .${ENV}.env
.secrets.env          # ← still last: secrets win over a host file
```

The engine substitutes both `${HOST}` and `${HOSTNAME}` (interchangeable) with
the machine hostname — an exported `HOSTNAME` wins (handy for CI / tests), else
the `hostname` command. A missing host file is silently skipped, so the chain is
identical on a box that has no override.

```sh
$ hostname
alice-laptop
$ cat .alice-laptop.env
DIRECTUS_LOG_LEVEL=trace      # just on Alice's machine
$ ./docker env-files
  …/.env
  …/.alice-laptop.env         # ← folded in, after .env, before .secrets.env
  …/.secrets.env
```

> **Slot it before `.secrets.env`, never after.** A host file is for per-machine
> *non-secret* tweaks (log levels, local paths, feature flags). Keeping it above
> secrets preserves the kit's invariant that `.secrets.env` wins — and a host
> file is **not** gitignored the way `.secrets.env` is, so never put a secret in
> one.

---

## dev / prod — one `COMPOSE_ENV` knob

`COMPOSE_ENV` (resolved shell > root `.env` > `dev`) drives the whole dev/prod
split from a single switch:

```sh
./docker compose up                 # dev (default)
COMPOSE_ENV=prod ./docker compose up # prod
```

It selects **three** things at once:

1. **The compose-file overlay**, via `COMPOSE_FILE` in `example.env`:

   ```sh
   COMPOSE_FILE=docker-compose.yml:docker-compose.${COMPOSE_ENV}.yml
   ```

   `docker-compose.${COMPOSE_ENV}.yml` resolves to `docker-compose.dev.yml` or
   `docker-compose.prod.yml` — the env-specific overlay, merged last so it wins.

   > **The plain `${COMPOSE_ENV}` token is safe here** — `COMPOSE_ENV` is known
   > when the chain is read (shell > `.env`, both *before* `COMPOSE_FILE` is
   > used), so no per-env re-pin is needed. The documented footgun (see
   > [`concepts.md`](concepts.md) → "Gotcha 1 — `${VAR:+...}`") is only the
   > **conditional** `${VAR:+suffix}` form, where the flag is set in a *later*
   > file than the one building `COMPOSE_FILE`. A straight substitution is fine.

2. **The root per-env tier** `.${ENV}.env` (`.dev.env` / `.prod.env`) in the
   chain — non-secret, env-specific root config, last-wins over `.env`. This is
   where the **`IS_DEV` convention** lives:

   ```sh
   # .dev.env            # .prod.env
   IS_DEV=true           IS_DEV=false
   ```

   `IS_DEV` is a plain value, not engine magic — the kit derives nothing; you
   set the pair and `COMPOSE_ENV` selects which file loads. Keep them in sync.

3. **Each subproject's per-service tier** `.<svc>.${COMPOSE_ENV}.env`, declared
   right in the subproject's `env_file:` so it works standalone too:

   ```yaml
   # web/docker-compose.yml
   env_file:
     - path: ./.web.env                       # env-agnostic (WEB_PORT)
       required: false
     - path: ./.web.${COMPOSE_ENV:-dev}.env   # per-env tier (.web.dev.env / .web.prod.env)
       required: false
   ```

   The kit's Layer-2 parser substitutes `${COMPOSE_ENV:-dev}` in the path, so a
   root-level `COMPOSE_ENV=prod` makes `web/.web.prod.env` resolve from the root
   too — the same one-knob switch reaches into every subproject.

---

## Profiles — optional services

Services with a `profiles:` key run only when their profile is selected;
unprofiled services always run. compose-envkit treats `COMPOSE_PROFILES` as pure
**passthrough** — the shim execs `docker compose "$@"` and compose reads
`COMPOSE_PROFILES` from the shell (or the root chain) itself. No kit state, no
helper to learn, nothing to keep in sync.

```yaml
# docker-compose.yml
services:
  tools:
    image: busybox
    profiles: [tools]      # OFF unless the 'tools' profile is selected
```

```sh
./docker compose up                          # web + api (unprofiled, always on)
COMPOSE_PROFILES=tools ./docker compose up    # + tools
# or pin a default in the root .env:  COMPOSE_PROFILES=tools
```

Document your profile catalog in `example.env` (as the blueprint does) so the
toggles are discoverable. Multi-membership is the native compose behaviour — a
service listing `profiles: [a, b]` runs under either, which is how you toggle a
bundled datastore independently of its app.

---

## Namespacing & renaming across subprojects

Layer-2 folds every discovered subproject `env_file:` into one
`COMPOSE_ENV_FILES`, **last-wins**, and those files **are** interpolated in chain
order — Layer 1 (root) first, then Layer 2 (subprojects). Two consequences:

**You CAN rename an upstream var.** A subproject can map a root var to the name
its binary expects, because the root value is defined earlier in the chain:

```sh
# web/.web.env (Layer 2)               root .env (Layer 1) defines SITE_URL earlier
APP_BASE_URL=${SITE_URL:-http://localhost}
```

From the root `${SITE_URL}` resolves to the root value; the `:-default` keeps the
subproject **standalone-safe** when nothing upstream provides it. (This is also
why `env-debug --value` is Layer-1-only — Layer-2 values can hold live `${...}`
refs that compose resolves but a plain shell `source` would mangle.)

**But bare names alias — prefix them.** Because all subproject env files merge
last-wins for *root* interpolation, two subprojects each defining a bare `PORT`
cross-talk: the later-discovered one wins. Prefix subproject-owned vars
(`WEB_PORT`, `API_PORT` — never a bare `PORT`) so they can't collide. A
subproject run *standalone* only sees its own file, so the alias bites only from
the root.

**Ordering caveat.** A rename resolves only if the referenced var appears
*earlier* in the merged chain. Root (Layer 1) → subproject (Layer 2) is safe;
sibling → sibling is not (Layer-2 discovery order is filesystem-dependent) —
route any cross-subproject value through the root chain instead of one
subproject reaching into another.

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

## Bootstrap — `init.sh`

`install.sh` drops a customizable `init.sh` (generated once, **never clobbered**)
for the one-time "set this repo up on a fresh machine" step. Out of the box it:

1. **Seeds env files** — copies each `example.X` → `.X` (`.env`, `.dev.env`,
   `.secrets.env`, …) only if missing; fill in real values afterwards.
2. **Fans out** — runs every immediate `<subdir>/init.sh`, so a monorepo root
   sets up each subproject in one shot (build extensions/themes, `mkdir` data
   dirs — whatever that subproject's own `init.sh` does). No service names are
   baked into the kit; it's discovery-based.
3. **Your steps** — a clearly marked section for project-specific setup, plus an
   opt-in, git-guarded `assume_unchanged` helper that hides local edits to
   *committed* demo env files from `git status`.

```sh
./init.sh        # idempotent — re-run any time
```

Deliberately POSIX `sh` and side-effect-light: **no `sudo`, no `chmod 777`, no
secrets written anywhere** — exactly the legacy compile-step pitfalls the kit
exists to avoid. Your project's `init.sh` is yours; the kit never overwrites it.

---

## Submodules & the `services/<svc>/` layout

A subproject can sit one (or more) levels deeper — the legacy `services/<svc>/`
shape — and/or be a **git submodule**. Neither is special to the kit:

- **Deeper nesting.** Discovery is a depth-bounded `find`, so
  `services/reports/docker-compose.yml` (find-depth 3) is reached at the default
  `COMPOSE_DEPTH=3`. The blueprint ships exactly this (`services/reports/`).
  Nest deeper — `services/<group>/<svc>/` — and bump `COMPOSE_DEPTH` to match
  (see [the depth knob](#how-the-discovery-reaches-across-the-include--compose_depth)).
  Include it from the root the same way: `include: [{ path: ./services/reports/docker-compose.yml }]`.
- **Submodules.** A subproject that is its own git repo (a submodule) is just a
  directory to the kit — discovery is `find`-by-glob, blind to `.git`. It runs
  unified (folded in from the root) or standalone (its own `./docker`, Option A
  or B) exactly like an in-tree subproject. Keep the subproject's `env_file:`
  paths relative to itself (the compose spec's rule, which the kit honors) so the
  same compose file works from the root and from inside the submodule.

> Migrating the legacy `services/<svc>/` submodules: point the root `include:`
> at each and ensure `COMPOSE_DEPTH` covers the deepest. The per-subproject
> `env_file:` values then resolve from the root — *provided each uses a plain
> relative path* (see [Migrating an existing monorepo](#migrating-an-existing-monorepo)).

---

## Migrating an existing monorepo

The kit reaches parity with the legacy env *features*, but a real legacy tree is
not always drop-in. Before a unified `./docker compose up` works, expect this
one-time, mechanical rework — validate the result with `./docker compose config`:

- **Relative `env_file:` paths.** The Layer-2 parser substitutes only
  `${COMPOSE_ENV}` in `env_file:` paths, so a legacy
  `env_file: ${SVC_DIR:-.}/.svc.env` pointer is emitted literally and never
  found. Rewrite each to a plain relative path (`./.svc.env`) — the compose-spec
  rule the kit relies on. (Run-from-anywhere pointers are replaced by the
  self-locating `./docker` + depth-bounded discovery.)
- **`include:` instead of a `COMPOSE_FILE` fragment list.** Replace a
  comma/colon-assembled `COMPOSE_FILE=${dc1},${dc2},…` with `include:` of each
  subproject compose, and move cross-cutting overlays (shared networks,
  `extra_hosts`, `depends_on`, static IPs) into the root `services:` block.
  Reserve `COMPOSE_FILE` for the `:docker-compose.${COMPOSE_ENV}.yml` selector.
- **Rename stray `docker-compose*.yml`.** Build-only variants (e.g.
  `docker-compose-yandex.yml`) match the discovery glob and would over-discover
  their `env_file:`. Rename them out of the glob (`yandex.compose.yml`).
- **Flatten nested defaults.** `${A:-${B:-c}}` is not parsed — flatten to
  `${COMPOSE_ENV:-dev}`. And note **secrets are now last-wins**: audit for a key
  set in both `.env` and `.secrets.env` (the new rule lets the secret win).
- **Out of scope (keep as-is, alongside the kit):** a Terraform `TF_VAR_*`
  fan-out, `pnpm`/`yarn` wrappers, and any non-compose tooling — the kit owns
  only the compose env-chain.

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

- **Discovery is filename + depth based, not `include:`-graph aware
  (over-discovery).** Layer-2 finds env files by globbing `docker-compose*.yml`
  within `COMPOSE_DEPTH` — it does **not** read your `include:` list or
  `COMPOSE_FILE`. So a stray compose that matches the glob (a CI variant
  `docker-compose.ci.yml`, a vendored `docker-compose-yandex.yml`, an old
  `docker-compose.bak.yml`) has its `env_file:` folded into the chain **even
  though you never `include:` it** — its vars can then win a last-wins collision
  for root interpolation. Symptom: an unexpected var shows up in
  `./docker env-files`. Fix: don't leave stray `docker-compose*.yml` files inside
  the tree (rename them so they don't match, e.g. `docker-compose.yml.bak`), or
  narrow `COMPOSE_DEPTH` so the search doesn't reach them.

- **Only `docker-compose*.yml` is discovered.** A subproject compose named
  `compose.yaml` / `compose.yml` (Compose's *other* default names) or any custom
  filename is **invisible** to Layer-2 — its `env_file:` will not resolve from
  the root, even well within `COMPOSE_DEPTH`. Symptom: a subproject port shows
  the `:-0` fallback from the root, yet the compose file plainly exists at a
  shallow depth. Fix: name subproject compose files `docker-compose*.yml` (or add
  a `docker-compose.yml` symlink to the real file).

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
- `test/smoke-monorepo.sh` — proves this end-to-end: cross-subproject Layer-2,
  both isolation options (A ride-on-parent / B self-contained), `COMPOSE_ENV`
  switching, the `COMPOSE_DEPTH` boundary, and the over-discovery / glob-naming
  limits documented above.
