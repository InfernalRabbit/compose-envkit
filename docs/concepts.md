# Concepts

How compose-envkit assembles `COMPOSE_ENV_FILES`, why it has to, and the two
interpolation gotchas you need to keep in mind. This is the "why it works"
companion to the task-oriented [integration](integration.md) and
[debug-flow](debug-flow.md) docs.

The engine is `lib/compose-env.sh` (installed as `scripts/compose-env.sh`),
invoked by the `./docker` shim. The `env_file:` parser is
`lib/parse-compose-env-files.sh`.

---

## The two-layer env-chain

`./docker` builds one comma-separated `COMPOSE_ENV_FILES` value from two layers
and exports it before handing off to `docker compose`. Docker Compose then loads
those files **in order, last-wins**, and uses the merged result as the
interpolation context for `${VAR}` references in the YAML.

### Layer 1 — the project chain

Listed in `<project>/.docker-env-chain`, one path per line, relative to the
project directory. Blank lines and `#` comments are ignored; leading/trailing
whitespace is trimmed. Each line may use `${ENV}` or `${COMPOSE_ENV}`, which the
engine substitutes textually before resolving the path. Non-existent files are
**silently skipped** (so a chain can list `.secrets.env` that only exists in
production without erroring in dev).

The shipped template (`templates/docker-env-chain`) is:

```
.env
.${ENV}.env

.secrets.env
```

If `.docker-env-chain` is **absent**, the engine falls back to the same three
built-in defaults: `.env`, `.${ENV}.env`, `.secrets.env`.

### `${ENV}` resolution

`${ENV}` / `${COMPOSE_ENV}` resolves the same way everywhere in the kit:

```
shell COMPOSE_ENV   >   COMPOSE_ENV= line in .env   >   "dev"
```

So `COMPOSE_ENV=prod ./docker compose up` selects `.prod.env`; with nothing set,
`.dev.env` is the overlay. (This mirrors how the `DC_PROD` make variable
pre-exports `COMPOSE_ENV=prod` — see [integration](integration.md).)

### Layer 2 — service `env_file:` discovery

After Layer 1, the engine runs `find` over the project (to `COMPOSE_DEPTH`
levels, default 3) for `docker-compose*.yml`, then calls
`parse-compose-env-files.sh` to extract every service's `env_file:` paths. Each
discovered path is normalized to an absolute path, de-duplicated against the
chain so far, and **appended after** Layer 1.

The parser understands all three `env_file:` spellings the compose spec allows:

```yaml
services:
  app:
    env_file: .app.env              # single scalar

  db:
    env_file:                       # short list
      - .db.env
      - .db.${COMPOSE_ENV:-dev}.env

  cache:
    env_file:                       # long-form list
      - path: .cache.env
        required: false
```

`${COMPOSE_ENV:-default}` and `${COMPOSE_ENV}` inside an `env_file:` path are
substituted with the resolved env value. Paths are taken **relative to the
directory of the compose file that declared them** (as the compose spec
requires), with any leading `./` stripped, then output de-duplicated with order
preserved.

> You do **not** list `env_file:` paths in `.docker-env-chain`. They are
> discovered from the YAML. `.docker-env-chain` is for the project-level chain
> only.

### The resulting order

```
COMPOSE_ENV_FILES = <Layer 1, in chain order> , <Layer 2 env_file:, discovery order>
                    └──────── earlier ────────┘   └──────── later (wins) ────────┘
```

`./docker env-files` prints this exact resolved list (one absolute path per
line) — it is the source of truth the debug tooling reads.

---

## The gap native compose doesn't close

This is the reason the kit exists. Docker Compose deliberately keeps two env
mechanisms separate:

- **The project-level env** (`--env-file`, or `COMPOSE_ENV_FILES`) **is** the
  interpolation context for `${VAR}` references in the compose YAML.
- A service's **`env_file:`** populates only the **container's runtime
  environment**. It is **not** read into the interpolation context.

The consequence: a value that lives only in a service `env_file:` is invisible
to compose-time interpolation. Given

```yaml
services:
  web:
    env_file: [./web.env]           # web.env contains APP_PORT=24680
    ports:
      - "${APP_PORT:-3000}:80"      # interpolated BEFORE web.env is read
```

native `docker compose config` resolves the published port to **3000** (the
`:-3000` fallback), because `${APP_PORT}` is interpolated before `web.env` is
ever loaded. This is by design and long-standing
([docker/compose#3435](https://github.com/docker/compose/issues/3435), open
since 2016) — it is not a bug you can configure away.

**What compose-envkit does:** because it appends `web.env` to
`COMPOSE_ENV_FILES` (Layer 2), the same file is now *also* part of the
interpolation context. So `${APP_PORT}` resolves to `24680`, and the runtime env
is unchanged (compose still loads `web.env` into the container as before). One
file, two roles, consistent value.

You can prove it in any installed project:

```
$ ./docker compose config | grep -E 'published|APP_PORT'
      APP_PORT: "24680"
        published: "24680"     # ← the env_file: value, not the :-3000 fallback
```

Same class of problem this fixes: a `redis` guard whose `REDIS_PASSWORD` comes
from a service `env_file:`, an image tag pinned in a service `env_file:`, etc.

### When you don't need Layer 2

If a value is referenced in the YAML via `${VAR}`, put it in the **project
chain** (`.env` / `.${ENV}.env`) — that's natively interpolated and needs no
kit. Layer 2 is specifically for values you keep in a *service* `env_file:`
(usually because that file also configures the container) yet *also* reference
at compose time. The kit lets you keep them in one place.

---

## Gotcha 1 — `${VAR:+...}` is expanded at `.env`-parse time

This trips people up when toggling optional compose overlays. Compose interpolates
each `.env`/env-file **line as it reads that file**, using whatever is already in
the environment at that moment — not after the whole chain has merged.

So a conditional overlay built with the `${FLAG:+:file}` idiom:

```sh
# in .env  — FLAG is still empty here
COMPOSE_FILE=docker-compose.yml${OVERLAY_TLS:+:docker-compose.tls.yml}:docker-compose.${COMPOSE_ENV}.yml
```

expands `${OVERLAY_TLS:+...}` at the moment `.env` is parsed. If `OVERLAY_TLS`
is only set later (say, in `.prod.env`), the overlay is **already dropped** from
`COMPOSE_FILE` by the time `.prod.env` loads — last-wins on the variable does
not retroactively re-run the `:+` expansion.

**The fix (used in the shipped `example.prod.env`):** make each environment
self-contained by re-pinning the full `COMPOSE_FILE` in that environment's
overlay, spelling out every overlay you want ON:

```sh
# in .prod.env — deterministic, no conditional that depends on load order
COMPOSE_FILE=docker-compose.yml:docker-compose.tls.yml:docker-compose.prod.yml
```

This is a last-wins **override** of the whole `COMPOSE_FILE` string, not a
re-interpolation — which is exactly why it works regardless of when the flag is
set. (For a flag you must set *before* `.env` is read, export it in the shell —
that's what `DC_PROD = COMPOSE_ENV=prod … ./docker compose` does.)

---

## Gotcha 2 — secrets and `${...}`-built URLs

Because the chain loads last-wins and interpolation is line-by-line, **do not
embed a secret into a URL you build in `.env`**:

```sh
# .env  — WRONG: REDIS_PASSWORD isn't loaded yet (it's in .secrets.env, later)
CACHE_URL=redis://:${REDIS_PASSWORD}@redis:6379
```

When `.env` is parsed, `.secrets.env` hasn't loaded, so `${REDIS_PASSWORD}`
expands to empty and `CACHE_URL` bakes in a blank password — a last-wins
override of `REDIS_PASSWORD` in `.secrets.env` cannot fix the already-built
string. Pass discrete `REDIS_HOST` / `REDIS_PORT` / `REDIS_PASSWORD` to the
service instead and let it assemble the URL at runtime. (The shipped
`example.env` calls this out explicitly.)

---

## `--value` reads the chain, not `env_file:`

`env-debug --value --var NAME` is the one debug mode that *sources* env files in
a real `sh` subshell (so `${VAR}` / `${VAR:-default}` / `${VAR:+...}` expand
exactly like compose). It deliberately sources **only the Layer-1 project chain**
(`.env` → `.${ENV}.env` → `.secrets.env`), never the Layer-2 `env_file:` paths.

Why: service `env_file:` files hold **bare compose-interpolation refs** like
`DATABASE_URL=${DATABASE_DB}` that only resolve at compose-merge time and are
unsafe to shell-source (they'd expand to empty, or under `set -u` would crash).
The project-level "info" variables you'd query with `--value`
(`COMPOSE_FILE`, `COMPOSE_PROJECT_NAME`, `SITE_URL`, …) all live in the project
chain, so this restriction is what makes `--value` safe to feed into
`Makefile`s. Shell-exported values still win over file values, matching compose
precedence. See [debug-flow](debug-flow.md) for usage.
