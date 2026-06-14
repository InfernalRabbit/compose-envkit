# The debug flow

`env-debug` answers the question "where does this value actually come from?"
without you having to read every env file by hand. It is **fully dynamic** — it
discovers your env files (via `./docker env-files`), your services (via
`./docker compose config --services`), and your `env_file:` directives (by
parsing the YAML). Nothing about your project is hardcoded.

Two ways to invoke it:

```sh
# via make (recommended — the targets wire the script path for you)
make env-debug
make env-debug-diff VAR=DATABASE_HOST
make env-debug-trace VAR=APP_PORT SERVICE=web

# directly (run from the project dir that has ./docker + the compose files)
sh scripts/env-debug.sh --chain
sh scripts/env-debug.sh --diff --var DATABASE_HOST
sh scripts/env-debug.sh --trace --var APP_PORT --service web
```

The examples below come from a minimal project: a `web` service with
`env_file: [./web.env]`, `ports: "${APP_PORT:-3000}:80"`, and a project chain of
`.env` → `.dev.env` → `.secrets.env`. `web.env` defines `APP_PORT=24680`;
`.env` sets `DATABASE_HOST=db`, `.dev.env` overrides it to `db-dev`.

> **Docker requirement.** `--effective` and `--trace` (and `make validate`) shell
> out to `./docker compose config`, so they need Docker Compose. The
> chain-reading modes — `--chain`, `--diff`, `--files`, `--value` — work with no
> Docker.

---

## Modes

Modes are mutually exclusive; one is selected per invocation (default `--chain`).

| Flag | `make` target | Needs Docker | Output |
|---|---|:---:|---|
| `--chain` | `make env-debug` | no | files loaded + load order + origin tags |
| `--diff` | `make env-debug-diff` | no | per-file add/override/repeat |
| `--effective` | `make env-debug-effective` | yes | final per-service values |
| `--files` | `make env-debug-files` | no | bare path list (machine-readable) |
| `--trace --var N` | `make env-debug-trace VAR=N` | yes | full resolution stack for one var |
| `--value --var N` | _(none — script/CLI use)_ | no | one resolved value, plain stdout |

### Filters (combine with any mode)

- `--service <name>` (alias `--container`) — restrict the `env_file:` side to one
  service. The name must be a real `docker compose config --services` value.
  Via make: `SERVICE=web`.
- `--var <NAME>` (alias `-V`) — track a single variable. In `--chain`/`--files`
  it shows only files that contain the var; in `--diff` only that key; in
  `--effective` only services where it's set; required for `--trace`/`--value`.
  Via make: `VAR=NAME`.

---

## `--chain` (default) — what loads, in what order

```
$ make env-debug

=== env chain — ckdemo (mode: chain) ===

  COMPOSE_ENV  = dev (from .env)
  Project dir  = /path/to/ckdemo

Project-level chain (./docker shim → COMPOSE_ENV_FILES, last wins)

    + .env                            [.docker-env-chain]
    + .dev.env                        [.docker-env-chain]
    + .secrets.env                    [.docker-env-chain]
    + ./web.env                       [compose env_file: web]

Container env_file: (docker compose, runtime)

  web:
    + ./web.env
```

`+` is a file that exists; `·` (dim) is one listed in the chain but missing on
disk. The dim origin tag says **why** a file is in the chain: `[.docker-env-chain]`
= Layer 1, `[compose env_file: web]` = discovered from service `web` (Layer 2).
A file that is both (rare) gets both tags. Add `--var NAME` to highlight which
files contain that variable, or `--service web` to focus one container.

---

## `--diff` — what each file adds, overrides, or repeats

Simulates the last-wins merge so you can see exactly who set what:

```
$ make env-debug-diff

Project-level (./docker shim)
  + new   ~ override   · same value repeated

  .env
      + COMPOSE_PROJECT_NAME = ckdemo
      + COMPOSE_ENV = dev
      + DATABASE_HOST = db

  .dev.env
      ~ DATABASE_HOST = db → db-dev

  .secrets.env
      + DATABASE_PASSWORD = s3cr3t

  ./web.env
      + APP_PORT = 24680
```

- `+` (green) — the key appears for the first time.
- `~` (yellow) — a later file overrides an earlier value (shows `old → new`).
- `·` (dim) — the same key repeats with the **same** value (redundant).

Narrow to one variable to see only its lineage:

```
$ make env-debug-diff VAR=DATABASE_HOST

  .env
      + DATABASE_HOST = db
  .dev.env
      ~ DATABASE_HOST = db → db-dev
  .secrets.env
      (no occurrences of DATABASE_HOST)
  ./web.env
      (no occurrences of DATABASE_HOST)
```

---

## `--effective` — final per-service values

Runs `./docker compose config` and shows the merged `environment:` block per
service, with every `${VAR:-default}` already resolved by compose:

```
$ make env-debug-effective

Effective env per service (via ./docker compose config)

  # web
      APP_PORT                       24680
      DB_HOST                        db-dev

  Full compose config: /tmp/…/config.yml
```

`APP_PORT = 24680` here is the proof Layer-2 fired: the value came from
`web.env` (a service `env_file:`), which native compose would not have used for
interpolation. Filter with `SERVICE=web` and/or `VAR=APP_PORT`. If the
`./docker compose config` call fails, this mode prints the compose error
(indented, first 20 lines) and exits non-zero.

---

## `--files` — bare path list (machine-readable)

No header, no color — one path per line, de-duplicated. Built for pipes:

```
$ make env-debug-files
.env
.dev.env
.secrets.env
./web.env

# e.g. grep every loaded file for a key
$ sh scripts/env-debug.sh --files | xargs grep -l APP_PORT
```

`VAR=NAME` restricts the list to files that actually contain that key.

---

## `--trace --var NAME` — the full resolution stack

The deepest mode: for one variable, in each service that defines it, it shows
**[1]** where it's set in a container `env_file:` (file:line + raw value),
**[2]** every `${REF}` that value contains and where each ref resolves in the
project chain (or which `:-default` fires), and **[3]** the effective value
compose computes.

A literal value:

```
$ make env-debug-trace VAR=APP_PORT

=== APP_PORT → web ===

  [1] Container env_file:
      ./web.env:1
         APP_PORT=24680

  (value has no references — literal)

  [3] Effective:
      APP_PORT = 24680
```

A value that references another variable:

```
$ make env-debug-trace VAR=APP_URL

=== APP_URL → web ===

  [1] Container env_file:
      ./web.env:2
         APP_URL=https://${SITE_HOST:-localhost}/app

  [2] References (project chain):
      ${SITE_HOST}
         + .env:4  SITE_HOST=demo.example.com

  [3] Effective:
      APP_URL = https://demo.example.com/app
```

In `[2]`: `+` means the ref is set in the project chain (with file:line); `~`
means it's unset and the `:-default` fires; `x` means unset with no default
(empty string). If a referenced value *itself* contains `${...}`, the trace flags
it transitively and points you at `trace --var <thatref>`.

`--trace` requires `--var`. Add `SERVICE=web` to restrict to one container. If
the variable isn't defined in any container `env_file:`, it tells you and
suggests `--diff --var NAME` to find where it does occur.

---

## `--value --var NAME` — one resolved value, plain stdout

Prints a single project-level variable's resolved value — no header, no color —
for consumption by `make` and scripts. It sources **only the Layer-1 project
chain** (`.env` → `.${ENV}.env` → `.secrets.env`) in load order inside an `sh`
subshell, so `${VAR}` / `${VAR:-default}` / `${VAR:+...}` expand exactly as
compose would, picking up overlay and secret layers, last-wins:

```
$ sh scripts/env-debug.sh --value --var DATABASE_HOST
db-dev

$ sh scripts/env-debug.sh --value --var COMPOSE_FILE
docker-compose.yml:docker-compose.dev.yml
```

A shell-exported value wins over the file value (matching compose precedence).
An unset variable yields an empty string (no crash). It deliberately does **not**
source container `env_file:` paths — those hold bare compose-refs that are unsafe
to shell-source. See
[concepts → `--value` reads the chain](concepts.md#--value-reads-the-chain-not-env_file).

---

## `./docker env-files`

Lower-level than env-debug, and what `--chain`/`--files` build on: it prints the
raw resolved `COMPOSE_ENV_FILES` (Layer 1 + discovered Layer 2), one **absolute**
path per line.

```
$ ./docker env-files
/path/to/ckdemo/.env
/path/to/ckdemo/.dev.env
/path/to/ckdemo/.secrets.env
/path/to/ckdemo/./web.env
```

(The `/./` segment on a discovered path is harmless — it mirrors the `./web.env`
the compose file declared and resolves to the same file.)

---

## Tab-completion

`make install-completions` prints the bash/zsh source lines. Once sourced:

```
make env-debug-trace VAR=<TAB>     # → variable names from the chain
make env-debug SERVICE=<TAB>       # → service names from compose
make env-deb<TAB>                  # → env-debug-* targets
```

The helper targets `make env-debug-vars` and `make env-debug-services` (the data
sources the completion uses) also work standalone if you want a plain list.
