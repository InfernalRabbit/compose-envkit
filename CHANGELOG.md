# Changelog

All notable changes to compose-envkit are documented here. This project adheres
to [Semantic Versioning](https://semver.org/) and the
[Keep a Changelog](https://keepachangelog.com/) format.

## [Unreleased]

Work toward monorepo feature parity — M1 (harden the core) and M2 (dev/prod
cohesion + per-machine overrides).

### Added

- **Per-machine host overrides (M2)**: `.docker-env-chain` entries now substitute
  `${HOST}` / `${HOSTNAME}` (the machine hostname; an exported `HOSTNAME` wins,
  else the `hostname` command) in both `lib/compose-env.sh` and the `bin/docker`
  inline fallback. A per-machine `.${HOSTNAME}.env` layer overrides the shared
  chain while staying **below** `.secrets.env`.
- **dev/prod cohesion (M2)** in `examples/monorepo/`: a `COMPOSE_FILE`
  `:docker-compose.${COMPOSE_ENV}.yml` overlay selector with `docker-compose.dev.yml`
  / `docker-compose.prod.yml`, per-service `.<svc>.${COMPOSE_ENV}.env` tiers, and
  root `.{dev,prod}.env` tiers carrying the `IS_DEV` convention — all driven by a
  single `COMPOSE_ENV` knob (no `${VAR:+...}` re-pin needed).
- Expanded **`test/smoke-monorepo.sh`** coverage (20 → 46 assertions): isolated
  `api/` + Option-B standalone, `COMPOSE_ENV=prod`, and the Layer-2
  depth/over-discovery/glob-name limits (M1); host overrides + dev/prod overlay,
  per-service tier, and root IS_DEV tier selection (M2). Shared `env_files_has`,
  `ef_index`, and `config_str` assertion helpers.
- **`docs/monorepo.md`** — Layer-2 discovery limits (filename+depth based, not
  `include:`-aware → over-discovery; `docker-compose*.yml`-glob only) plus new
  "Per-machine overrides" and "dev / prod — one COMPOSE_ENV knob" sections.

### Changed

- `templates/docker-env-chain` documents the optional per-machine
  `.${HOSTNAME}.env` layer and the new `${HOST}` / `${HOSTNAME}` substitution.

## [0.1.0] — 2026-06-15

Initial release. Extracted and generalized from the SmartDriver infra tooling
into a portable, project-agnostic kit.

### Added

- **`bin/docker`** — universal, self-locating POSIX shim. Resolves the env
  chain, assembles `COMPOSE_ENV_FILES`, and dispatches `compose` / `env-files`
  / passthrough. Locates its lib via own `scripts/` then the parent's, with a
  minimal inline fallback for standalone-cloned subprojects.
- **`lib/compose-env.sh`** — `COMPOSE_ENV_FILES` assembly (Layer 1 project
  chain + Layer 2 `env_file:` auto-discovery), project-agnostic.
- **`lib/parse-compose-env-files.sh`** — portable-awk parser for `env_file:`
  directives in `docker-compose*.yml` (single value, short list, and long-form
  `path:`/`required:`), with `${COMPOSE_ENV:-default}` substitution.
- **`lib/env-debug.sh`** — env-chain inspector with all modes: `--chain`,
  `--diff`, `--effective`, `--files`, `--trace VAR`, `--value VAR`.
- **`mk/compose.mk`** + **`mk/env-debug.mk`** — neutral Make base
  (`DC`/`DC_PROD`/`PLATFORM`/`validate`/`help`) and the `env-debug*` targets.
- **`templates/`** — neutral `.docker-env-chain` + `example.*` env templates.
- **`completions/`** — bash/zsh tab-completion for the `make env-debug-*`
  targets.
- **`install.sh`** — idempotent integrator: vendors `lib/*.sh` + `mk/*.mk`
  into `<target>/scripts/`, installs `./docker`, generates `.docker-env-chain`
  and `example.*` without clobbering existing real env files. Supports
  `--help` and `--dry-run`.
- **`test/lint.sh`** — `sh -n` + optional `shellcheck` on every shipped script.
- **`test/smoke.sh`** — end-to-end: installs into a temp project and asserts
  Layer-2 `env_file:` interpolation resolves via `./docker compose config`,
  exercises every `env-debug` mode and `./docker env-files`, and runs from both
  the project root and a subproject subdir.
- **`README.md`**, **`AGENTS.md`**, **`docs/`** — human and LLM integration
  guides covering the architecture, the `env_file:`→interpolation gap, and
  portability constraints.

### Portability

- POSIX `sh` throughout (no bashisms); portable awk; path resolution via
  `CDPATH= cd … && pwd` (no GNU `readlink -f` / `realpath`); `printf` over
  `echo -e`. Every shipped script passes `sh -n`.
- Targets GNU make; Windows via WSL2 or Git-Bash. BSD-make and BSD userland
  caveats documented in `docs/portability.md`.

[0.1.0]: https://example.com/compose-envkit/releases/tag/v0.1.0
