# Changelog

All notable changes to compose-envkit are documented here. This project adheres
to [Semantic Versioning](https://semver.org/) and the
[Keep a Changelog](https://keepachangelog.com/) format.

## [Unreleased]

### Changed — `env_file:` is runtime-only (Layer-2 debug-only) ⚠ behavior change

**BREAKING (pre-1.0).** A service `env_file:` is no longer folded into
`COMPOSE_ENV_FILES`. The run path (`cenvkit compose`, `cenvkit env-files`) now sets
`COMPOSE_ENV_FILES` to the **Layer-1 project chain only**; service `env_file:`s stay
**runtime-only** (native Docker — per-service, isolated). A `${VAR}` defined only in
a service `env_file:` therefore **falls back** at the run — reversing the earlier
behavior that resolved it via folded env_files.

Why: folding every service's `env_file:` into one global `COMPOSE_ENV_FILES`
collapsed shared keys into a single project-wide value (a `${PORT}` collision
footgun), because Compose interpolates the whole YAML against one global env map.
Values meant for `${VAR}` interpolation belong in the Layer-1 chain.

`env-debug` is repurposed from "simulate the fold" to a **gap-detector**:

- `--trace --var V` flags the gap when `V` is referenced in the YAML but resolvable
  only from a service `env_file:` (shows the runtime value + the fix).
- `--effective` interpolates inline `environment:` against Layer 1, so it shows the
  **true** final container value (never a resolution the run won't produce).
- `--files` is now a two-group view: interpolation (`COMPOSE_ENV_FILES`, Layer 1) +
  runtime-only (service `env_file:` paths, by service).

Migration: if you relied on a service `env_file:` value feeding `${VAR}`
interpolation, move that var into the Layer-1 chain
(`.env`/`.<env>.env`/`.secrets.env`); `cenvkit env-debug --trace --var <V>` points
out each gap. Spec:
[`docs/superpowers/specs/2026-06-17-cenvkit-layer2-debug-only-design.md`](docs/superpowers/specs/2026-06-17-cenvkit-layer2-debug-only-design.md).
Acceptance grew to **75** smoke-monorepo assertions.

### Removed

The deprecated POSIX-`sh` kit (the self-locating `docker` shim, the `lib/` engine
+ parser + debugger, the `mk/` Make glue, the `templates/`, the `completions/`,
the installer, the shell smoke/lint suites, and the sh-era docs) is removed —
`cenvkit` (the Go CLI) is the only implementation. The `examples/monorepo`
blueprint is now cenvkit-driven (Makefiles removed).

### rich provenance — `env-debug` v2

`cenvkit env-debug` is now **provenance-backed and daemon-free**: it loads the
compose model in-process (compose-go) and answers, for any variable, *which file
set the winning value, what it shadowed, and where `${VAR}` took effect*
(service/field → resolved value), and for any service, *its effective environment
with the source of each value* (`env_file:` vs inline `environment:`). Human
output by default; `--json` emits the structured `Report` for tooling/CI. This
supersedes v1's raw `--value`/`--trace`; the `--diff` flag is **removed**
(superseded by `--trace` + `--effective`); and `--effective` no longer shells out
to `docker compose config`. Parsing uses compose-go's own `dotenv` + `template`
packages (docker-compose parity); compose-go stays isolated behind
`internal/engine` (CI seam check). Acceptance grew to **68** smoke-monorepo
assertions. Full reference: [`docs/cenvkit.md`](docs/cenvkit.md).

### cenvkit — Go rewrite (v1)

The engine is rewritten as a Go CLI, **`cenvkit`**, built on Docker's own compose
loader (`github.com/compose-spec/compose-go/v2`, pinned v2.11.0). It assembles
`COMPOSE_ENV_FILES` from the real, include-aware, interpolated model and `exec`s
`docker compose` — eliminating the hand-rolled `awk`/`sed` engine's whole bug
class (no glob over-discovery; `${SVC_DIR}`/nested `${...}` resolve; no
sed-injection). Dual distribution: `go install`, `go run …@latest`, or a vendored
POSIX `cenvkit` shim. Commands: `compose`, `env-files`, `env-debug`, `validate`,
`init`, `version`. Full reference: [`docs/cenvkit.md`](docs/cenvkit.md).

- **Behavior (documented):** a missing *required* `env_file:` is lenient at chain
  assembly and upstream-fatal at the real `docker compose` run (D1); variable
  precedence is `docker compose`'s last-wins over `COMPOSE_ENV_FILES` (cenvkit
  only orders the file list — "secrets last" is a within-chain guarantee, not a
  cross-layer one); `COMPOSE_DEPTH` is accepted-but-ignored (the include-graph
  makes depth-glob obsolete); an `env_file:` *path* may reference Layer-1/chain
  vars only (single-pass, §4a).
- **Acceptance:** the `examples/monorepo` smoke suites are ported to drive
  `cenvkit` (**N=60**; the legacy depth-knob assertion 11.2 is dropped as
  untestable under the include-graph; scenarios 9/10/22 invert — a non-included
  subproject is not discovered). Table-driven unit tests per package; compose-go
  isolated behind `internal/engine` (CI-enforced seam check).

### Deprecated

- The POSIX-`sh` kit was **deprecated** in favor of `cenvkit`, and is now
  **removed** (see Removed, above).

### Earlier — monorepo feature parity (sh kit, M1–M5)

Work toward monorepo feature parity — M1 (harden the core), M2 (dev/prod
cohesion + per-machine overrides), M3 (profiles + namespacing), M4 (bootstrap),
M5 (deep `services/<svc>/` nesting + submodules).

### Added

- **Deep nesting + submodules (M5)**: the blueprint ships a `services/reports/`
  subproject nested one level deeper (the legacy `services/<svc>/` shape),
  included from the root — proving cross-subproject Layer-2 reaches it at the
  default `COMPOSE_DEPTH=3`. New `docs/monorepo.md` "Submodules & the
  `services/<svc>/` layout" section (a subproject can be a git submodule —
  discovery is `find`-by-glob, blind to `.git`). smoke-monorepo grew to 58
  (deep `services/reports` resolves `REPORTS_PORT` from the root; a `.git`
  gitlink-shaped subproject is still discovered).


- **Bootstrap `init.sh` (M4)**: `install.sh` now generates a customizable,
  executable `init.sh` (never clobbered) — a project-agnostic one-time bootstrap
  that seeds real env files from `example.*` (no-clobber), fans out to each
  immediate `<subdir>/init.sh`, and ships an opt-in git-guarded `assume_unchanged`
  helper. Deliberately POSIX sh with **no sudo / chmod 777 / persisted secrets**
  (the legacy compile-step pitfalls). New `templates/init.sh`; `do_generate`
  gained `--exec`; `test/lint.sh` now also lints `templates/*.sh`. Documented in
  a "Bootstrap — init.sh" section of `docs/monorepo.md`.


- **Profiles (M3)**: documented `COMPOSE_PROFILES` as pure passthrough (the shim
  forwards it to `docker compose` unchanged — no kit state). `examples/monorepo/`
  ships an optional `tools` service behind `profiles: [tools]`, a profile catalog
  in `example.env`, and a "Profiles" section in `docs/monorepo.md`.
- **Namespacing guidance (M3)**: `docs/monorepo.md` "Namespacing & renaming"
  section — corrected from the earlier assumption that env_file values are
  literal. They ARE interpolated in the `COMPOSE_ENV_FILES` context (chain order,
  Layer 1 before Layer 2), so a subproject CAN rename an upstream var
  (`NEW=${ROOT_VAR:-default}`, standalone-safe via `:-`); bare names still alias
  last-wins (prefix them), and only earlier-in-chain refs resolve.
- smoke-monorepo grew to 50 assertions (profiles on/off via `COMPOSE_PROFILES`;
  namespacing rename via the chain).


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

### Fixed

Found by an adversarial multi-agent parity review of M1–M5:

- **Host-token sed-injection** (M2 regression): `${HOST}` / `${HOSTNAME}` was
  spliced unescaped into a sed program, so a hostname containing `|` or `&`
  crashed the engine. Now sanitized to `[A-Za-z0-9._-]` in both
  `lib/compose-env.sh` and `bin/docker`.
- **CRLF / whitespace in `COMPOSE_ENV`**: resolution from `.env` now strips a
  trailing CR (Windows/WSL checkouts) and surrounding whitespace, and no longer
  truncates a value containing `=` (`cut -d=` → `sed`). Both files.
- **`init.sh` fan-out**: runs each subproject `init.sh` via `sh` (no `+x`
  dependency) and skips symlinked dirs (avoids cycles).
- **Test guards strengthened**: a missing Layer-1 `.env` now fails (was a silent
  `info`); the `IS_DEV` check asserts the engine-rendered value (a service
  consuming `${IS_DEV}`) instead of grepping the just-copied fixture; added a
  real `.git`-directory submodule fixture. smoke-monorepo now 61 assertions.

### Documented

- `docs/monorepo.md` "Migrating an existing monorepo" — honest, mechanical
  rework a real legacy tree needs (relative `env_file:` paths, `include:` instead
  of `COMPOSE_FILE` fragment assembly, renaming stray `docker-compose*.yml`,
  flattening nested defaults, secrets-now-last-wins; Terraform/pnpm/yarn out of
  scope). Parity is with the env *features*, not always drop-in.

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
