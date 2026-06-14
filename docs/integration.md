# Integration

How to wire compose-envkit into a project — the automated `install.sh` path, the
manual path, the root-vs-subproject choices, and the overlay/secret conventions.

For the *why* behind the chain, see [concepts](concepts.md).

---

## The install layout contract

After integration, a target project has this layout (the "install layout
contract" — the shim and the make includes all rely on these exact paths):

```
<project>/docker                       # self-locating shim
<project>/scripts/compose-env.sh       # COMPOSE_ENV_FILES assembly (the engine)
<project>/scripts/parse-compose-env-files.sh
<project>/scripts/env-debug.sh
<project>/scripts/compose.mk           # `include scripts/compose.mk`
<project>/scripts/env-debug.mk
<project>/scripts/completions/*
<project>/.docker-env-chain            # generated if missing
<project>/example.env  example.prod.env  example.secrets.env
```

Note the lib `*.sh`, the `*.mk`, and `completions/` all land **flattened** under
`scripts/` — that's what lets the `./docker` shim and `include scripts/compose.mk`
find everything by plain name.

---

## Automated: `install.sh`

```sh
sh /path/to/compose-envkit/install.sh [target-dir]      # default: .
```

Idempotent. Step by step it:

1. **Vendors** `lib/*.sh` (chmod +x) and `mk/*.mk` into `<target>/scripts/`.
2. **Vendors** `completions/*` into `<target>/scripts/completions/`.
3. **Copies** `bin/docker` → `<target>/docker` (chmod +x).
4. **Generates** `<target>/.docker-env-chain` from the template — **only if
   absent**. It never clobbers a project's real chain.
5. **Generates** `<target>/example.*` from the templates — **only if absent**.
   It never writes a real `.env` / `.secrets.env`.

Vendored payload (steps 1–3) is **always overwritten** — it's owned by the kit,
so re-running `install.sh` refreshes the engine. Generated files (steps 4–5) are
**never overwritten** — your real config is safe.

Then it prints the next steps: the `include scripts/compose.mk` line, the
`cp example.* → real` commands, the completion hint, and the subproject note.

### Flags

| Flag | Effect |
|---|---|
| `--help`, `-h` | print the installer banner and exit |
| `--dry-run`, `-n` | print every action (`mkdir`/`copy`/`create`/`skip`) without writing anything |

`--dry-run` tolerates a not-yet-existing target so you can preview a fresh
install. A real (non-dry) run requires the target dir to already exist. The
installer refuses to install the kit into its own source directory.

### After install — finish wiring

```sh
cd <target>

# 1. one-line Makefile include (pulls in DC/DC_PROD/PLATFORM/validate/help
#    and, transitively, the env-debug* targets)
echo 'include scripts/compose.mk' >> Makefile

# 2. real env files from the templates
cp example.env         .env           # fill non-secret defaults
cp example.secrets.env .secrets.env   # fill secrets — gitignored
# cp example.prod.env  .prod.env      # on the prod host, if you use it

# 3. verify
./docker env-files          # the resolved chain
./docker compose config     # interpolation, with the env_file: layer
make env-debug              # inspect the chain
make validate               # compose config check (dev + prod)
```

Add a `.gitignore` for the runtime files (the kit's own `.gitignore` is a good
template): ignore `.env`, `.*.env`, `.secrets.env`; keep `example.*`.

---

## Manual integration

If you'd rather not run `install.sh`, replicate the layout contract by hand:

1. Copy `lib/compose-env.sh`, `lib/parse-compose-env-files.sh`,
   `lib/env-debug.sh`, `mk/compose.mk`, `mk/env-debug.mk` into
   `<project>/scripts/` (keep the filenames; `chmod +x` the `.sh` files).
   Optionally copy `completions/*` into `scripts/completions/`.
2. Copy `bin/docker` → `<project>/docker`, `chmod +x`.
3. Copy `templates/docker-env-chain` → `<project>/.docker-env-chain` and edit
   it for your chain (or delete it to use the built-in defaults).
4. Copy `templates/example.*` to `<project>/`, then
   `cp example.env .env` etc. and fill them in.
5. Add `include scripts/compose.mk` to your `Makefile`.

The make includes resolve the script paths **relative to the `.mk` file's own
location** (via `$(dir $(lastword $(MAKEFILE_LIST)))`), so the include works
from the repo root and from a subproject that vendors its own `scripts/` alike.

---

## Root vs. isolated-subproject setup

The `./docker` shim self-locates the engine in this order (first found wins):

1. `<shim-dir>/scripts/compose-env.sh` — a fully vendored copy
2. `<shim-dir>/../scripts/compose-env.sh` — the parent repo's install
3. an **inline Layer-1-only fallback** — when neither is reachable (a subproject
   cloned standalone). This fallback reads `.docker-env-chain` (or the built-in
   defaults) but does **not** do Layer-2 `env_file:` discovery.

`PROJECT_DIR` is always the shim's own directory, so everything resolves
relative to where `./docker` lives.

**Root project** — run `install.sh <root>`. Done.

**Subproject — option A (ride on the parent):** drop a copy of the root's
`./docker` into the subproject dir. It walks up to the root's `scripts/`. Zero
extra files, full Layer-2 support.

```sh
cp <root>/docker <root>/sub/docker && chmod +x <root>/sub/docker
cd <root>/sub && ./docker compose config     # resolves sub/'s own env files
```

**Subproject — option B (self-contained):** run `install.sh <subproject-dir>`
so the subproject gets its **own** `scripts/` copy. Use this when the subproject
must work when cloned on its own (the inline fallback gives Layer-1 only; a full
vendored copy gives Layer-2).

Each subproject resolves its **own** `.docker-env-chain`, its own
`docker-compose*.yml`, and its own `env_file:` paths — running from the root and
running from a subproject are fully independent.

---

## Overlays and secrets conventions

### Compose-file overlays via `COMPOSE_FILE`

Select overlays by environment in `COMPOSE_FILE`. The base file is always
present; the per-environment overlay is keyed off `${COMPOSE_ENV}`:

```sh
# .env
COMPOSE_FILE=docker-compose.yml:docker-compose.${COMPOSE_ENV}.yml
```

For an optional overlay, the `${FLAG:+:file}` idiom expands to `:file` only when
`FLAG` is set non-empty:

```sh
# .env
OVERLAY_TLS=
COMPOSE_FILE=docker-compose.yml${OVERLAY_TLS:+:docker-compose.tls.yml}:docker-compose.${COMPOSE_ENV}.yml
```

> **Gotcha:** `${FLAG:+…}` is expanded at the moment `.env` is parsed. A flag you
> only set in a later overlay (e.g. `.prod.env`) is already gone from
> `COMPOSE_FILE` by then. The robust pattern is to **re-pin the full
> `COMPOSE_FILE`** in each environment's overlay — exactly what
> `example.prod.env` does. Details and the full explanation are in
> [concepts → Gotcha 1](concepts.md#gotcha-1----var-is-expanded-at-env-parse-time).

### Production via `DC_PROD`

`mk/compose.mk` defines, neutrally:

```make
DC      ?= ./docker compose
DC_PROD ?= COMPOSE_ENV=prod ./docker compose
```

`DC_PROD` pre-exports `COMPOSE_ENV=prod` in the shell **before** `.env` is read,
so `.prod.env` becomes the active overlay and any `${COMPOSE_ENV}` in
`COMPOSE_FILE` resolves to `prod`. To force a flag-driven overlay on every prod
call (belt-and-suspenders), override `DC_PROD` in **your** Makefile:

```make
DC_PROD = COMPOSE_ENV=prod OVERLAY_TLS=true ./docker compose
```

…or drive it purely from `.prod.env`'s re-pinned `COMPOSE_FILE` and keep
`DC_PROD = COMPOSE_ENV=prod ./docker compose`.

### Secrets

Secrets live in `.secrets.env` — **last** in the chain so they win, and
**gitignored**. Commit `example.secrets.env` (with `CHANGE_ME` placeholders)
instead. Don't bake a secret into a `${...}`-built URL in `.env` (it interpolates
before `.secrets.env` loads → blank); pass discrete fields. See
[concepts → Gotcha 2](concepts.md#gotcha-2----secrets-and--built-urls).

### What stays in your Makefile (not the kit)

`compose.mk` is intentionally project-agnostic. It provides only
`DC`/`DC_PROD`/`PLATFORM`/`DBP`/`HOSTNAME`, `validate`, `help`, and the
env-debug targets. Service names, hostnames, and targets like `dev`/`prod`/
`deploy`/`build`/`package` belong in **your** Makefile — define them using
`$(DC)` / `$(DC_PROD)`. Override `PLATFORM`, `DC`, or `DC_PROD` **before** the
`include scripts/compose.mk` line.
