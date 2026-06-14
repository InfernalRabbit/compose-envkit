# compose-envkit

A small, dependency-free toolkit that drops a **Docker Compose env-chain +
service `env_file:` discovery + a debug flow** into any project — on any
POSIX-capable OS.

It closes one specific gap that native Docker Compose leaves open: a service's
`env_file:` paths populate the **container runtime** environment, but they do
**not** participate in compose-time `${VAR}` interpolation of the YAML itself.
compose-envkit auto-discovers those `env_file:` paths and feeds them back into
interpolation, so `ports: "${APP_PORT:-3000}:80"` resolves to the `APP_PORT`
your service `env_file:` actually defines instead of silently falling back to
`3000`. (That's the whole reason the kit exists — see
[The gap](#the-gap-native-compose-doesnt-close).)

It is a portable extraction of the SmartDriver infra tooling: pure POSIX `sh`,
portable `awk`, GNU make. No Python, no Node, no extra binaries.

---

## Install in one command

From a complete checkout of the kit, point `install.sh` at your project:

```sh
sh /path/to/compose-envkit/install.sh /path/to/your/project
```

That vendors the engine into `your-project/scripts/`, drops a self-locating
`./docker` shim at the project root, generates a `.docker-env-chain` and
`example.*` env templates (it never clobbers a real `.env`/`.secrets.env`), and
prints the one-line Makefile include plus next steps. It is idempotent — re-run
it any time to refresh the vendored scripts.

Then, in your project:

```sh
cp example.env         .env            # fill in non-secret defaults
cp example.secrets.env .secrets.env    # fill in secrets (stays gitignored)
echo 'include scripts/compose.mk' >> Makefile

./docker env-files        # see the resolved env chain
./docker compose config   # interpolation now includes the env_file: layer
make env-debug            # inspect the chain
```

`--dry-run` (`-n`) prints every action without writing, and `--help` (`-h`)
shows the full installer banner. Manual integration is documented in
[`docs/integration.md`](docs/integration.md).

---

## What it is, and why

Modern Docker Compose already handles most of what a hand-rolled wrapper used to
do — `COMPOSE_ENV_FILES` (last-wins project chain), `COMPOSE_FILE` overlays,
`DOCKER_DEFAULT_PLATFORM`. compose-envkit wraps those for ergonomics, but it
exists for the **one** job that has no native equivalent.

### The gap native compose doesn't close

Docker Compose keeps two things deliberately separate:

| Layer | Populates | Used for compose-time `${VAR}` in the YAML? |
|---|---|---|
| Project-level env (`--env-file` / `COMPOSE_ENV_FILES`) | interpolation context | **yes** |
| A service's `env_file:` | the container's runtime env | **no** |

So if `APP_PORT` lives only in a service's `env_file:` and you write
`ports: "${APP_PORT:-3000}:80"`, compose interpolates `${APP_PORT}` **before**
that `env_file:` is ever read — you silently get the `:-3000` fallback. This is
an intentional, long-standing design split upstream
([docker/compose#3435](https://github.com/docker/compose/issues/3435), open
since 2016).

compose-envkit's engine parses every `docker-compose*.yml`, extracts each
service's `env_file:` paths, and folds them into `COMPOSE_ENV_FILES` **in
addition** to the project chain. Now the same files that configure the container
at runtime also feed compose-time interpolation, last-wins. That's Layer 2.
Concepts in full: [`docs/concepts.md`](docs/concepts.md).

---

## The env-chain

Two layers feed `COMPOSE_ENV_FILES`, in order, last-wins:

**Layer 1 — the project chain.** Listed in `.docker-env-chain` (or built-in
defaults when that file is absent). The default chain is:

```
.env             # non-secret defaults (committed via example.env)
.${ENV}.env      # per-environment overlay: .dev.env / .prod.env / …
.secrets.env     # secrets, gitignored — loaded LAST so it wins
```

`${ENV}` (alias `${COMPOSE_ENV}`) is resolved as **shell `COMPOSE_ENV`
> `.env`'s `COMPOSE_ENV` > `"dev"`**. Non-existent files are silently skipped.

**Layer 2 — service `env_file:` paths.** Auto-discovered from your
`docker-compose*.yml` files (searched to `COMPOSE_DEPTH` levels, default 3),
`${COMPOSE_ENV:-default}`-substituted, normalized and de-duplicated, then
appended after Layer 1. These files are already declared in your YAML — the kit
simply also makes them visible to interpolation.

```
  shell COMPOSE_ENV / .env / "dev"  ─┐
                                     ├─►  ${ENV} substitution in .docker-env-chain
  .docker-env-chain (Layer 1)  ──────┘            │
                                                  ▼
  docker-compose*.yml env_file: (Layer 2) ──►  COMPOSE_ENV_FILES  ──►  docker compose
```

See `./docker env-files` for the exact resolved list in your project.

---

## Run from root AND from a subproject

The `./docker` shim is **self-locating**. It finds the engine in this order:

1. its own `scripts/compose-env.sh` (a fully vendored copy), then
2. the parent directory's `scripts/compose-env.sh` (a shim that rides on the
   repo root's install), then
3. a minimal **inline fallback** (Layer-1 only) for a subproject cloned
   standalone with no reachable `scripts/`.

So you have two clean options for a subproject:

- **Ride on the parent** — drop a copy of `./docker` into the subproject dir;
  it walks up to the repo root's `scripts/`. Zero extra files.
- **Self-contained** — run `sh install.sh <subproject-dir>` to give the
  subproject its own `scripts/` copy, so it works even when cloned on its own.

Either way, `./docker` runs with `PROJECT_DIR` set to **its own directory**, and
all env-chain resolution and `env_file:` discovery happen relative to that — so
running from the root and running from a subproject each resolve their own
files correctly.

---

## The debug flow at a glance

`env-debug` (via `make env-debug*` or `sh scripts/env-debug.sh <mode>`) inspects
the chain dynamically — it never hardcodes your variable or service names.

| Mode | What it shows |
|---|---|
| `--chain` (default) | which env files load, in what order, tagged by origin (Layer 1 / Layer 2) |
| `--diff` | per file: what each one **adds** (`+`), **overrides** (`~`), or **repeats** (`·`) |
| `--effective` | final per-service values via `./docker compose config` (every `${VAR:-default}` resolved) |
| `--files` | bare list of loaded paths (machine-readable; for `grep`/`xargs`) |
| `--trace --var NAME` | the full call stack for one variable: where it's defined, its `${REF}`s, and the effective value |
| `--value --var NAME` | one resolved project-level value, plain stdout (for `make`/scripts) |

Filters `--service <name>` and `--var <NAME>` combine with any mode. Full
walkthrough with real output: [`docs/debug-flow.md`](docs/debug-flow.md).

```sh
make env-debug                          # the chain, with origins
make env-debug-diff VAR=DATABASE_HOST   # who set/overrode DATABASE_HOST
make env-debug-effective SERVICE=web    # final values compose will use for web
make env-debug-trace VAR=APP_PORT       # the whole resolution stack
```

---

## Requirements

- **POSIX `sh`** and **portable `awk`** (both standard on Linux/macOS/BSD). No
  bashisms; no GNU-only `readlink -f` / `realpath` / `sed -i`.
- **GNU make** for the `make` targets (BSD-make caveats in
  [`docs/portability.md`](docs/portability.md)).
- **Docker Compose** for the compose-touching modes (`--effective`, `--trace`,
  `make validate`, and `./docker compose`). The Layer-2 `env_file:` discovery
  relies on `env_file: required:` support, so target **Docker Compose ≥ 2.24.0**
  (Jan 2024). Chain-only modes (`--chain`, `--diff`, `--files`, `--value`) work
  without Docker.
- **Windows:** via **WSL2** or **Git-Bash**. No native PowerShell in this
  version — see [`docs/portability.md`](docs/portability.md).

---

## Layout

```
compose-envkit/
├── bin/docker                       # universal self-locating shim
├── lib/
│   ├── compose-env.sh               # COMPOSE_ENV_FILES assembly (the engine)
│   ├── parse-compose-env-files.sh   # portable-awk env_file: parser (Layer 2)
│   └── env-debug.sh                 # the debug-flow inspector (all modes)
├── mk/
│   ├── compose.mk                   # DC / DC_PROD / PLATFORM / validate / help
│   └── env-debug.mk                 # env-debug* + completion targets
├── templates/                       # .docker-env-chain + example.* env files
├── completions/                     # bash/zsh tab-completion for make targets
├── install.sh                       # idempotent integrator
├── test/{smoke.sh,lint.sh}          # end-to-end + static checks
└── docs/                            # concepts · integration · debug-flow · portability
```

After `install.sh`, your project gets `./docker`, `scripts/` (the flattened
engine + `.mk` files + `completions/`), `.docker-env-chain`, and `example.*`.

---

## Documentation

- [`docs/concepts.md`](docs/concepts.md) — the env-chain order, Layer-2
  discovery, the `env_file:`→interpolation gap, and the read-time
  `${VAR:+...}` gotcha.
- [`docs/integration.md`](docs/integration.md) — `install.sh` flow and manual
  integration; root vs isolated-subproject setup; overlay/secret conventions.
- [`docs/debug-flow.md`](docs/debug-flow.md) — every `env-debug` mode with
  example invocation and output.
- [`docs/portability.md`](docs/portability.md) — POSIX guarantees, Windows via
  WSL2/Git-Bash, BSD-vs-GNU caveats.
- `AGENTS.md` — integration guide for AI agents.

---

## License

MIT — see [`LICENSE`](LICENSE).
