# Changelog

All notable changes to compose-envkit are documented here. This project adheres
to [Semantic Versioning](https://semver.org/) and the
[Keep a Changelog](https://keepachangelog.com/) format.

## [Unreleased]

Work toward monorepo feature parity (M1 — harden the core).

### Added

- Expanded **`test/smoke-monorepo.sh`** coverage: isolated `api/` (Option A,
  asserts sibling-blindness), self-contained subproject via `install.sh`
  (Option B, own engine still does Layer-2), `COMPOSE_ENV=prod` assembly, and
  characterization of the Layer-2 `COMPOSE_DEPTH` boundary, over-discovery, and
  the `docker-compose*.yml` glob limit. Shared `env_files_has` assertion helper.
- **`docs/monorepo.md`** — documented Layer-2 discovery limits: filename + depth
  based (not `include:`-graph aware → over-discovery of stray
  `docker-compose*.yml`) and glob-only matching (`compose.yaml` / custom-named
  compose files are missed).

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
