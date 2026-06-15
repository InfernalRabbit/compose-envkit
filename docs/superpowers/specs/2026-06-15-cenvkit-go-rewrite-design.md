# cenvkit — Go rewrite design (compose-envkit → `cenvkit`)

Status: approved direction (brainstorming), 2026-06-15. Next: implementation plan.

## 1. Context & motivation

`compose-envkit` today is pure POSIX `sh` + portable `awk` + GNU make. It closes
one real gap (a service `env_file:` is invisible to compose-time `${VAR}`
interpolation) and we drove it to monorepo feature parity across M1–M5 (host
overrides, dev/prod cohesion, profiles, bootstrap, deep `services/<svc>/`
nesting; 61-assertion smoke suite). An adversarial multi-agent review confirmed
the parity but also exposed the **engine ceiling**: the hand-rolled `awk` YAML
parser only substitutes `${COMPOSE_ENV}` (not `${SVC_DIR}` pointers or nested
`${A:-${B:-c}}`), is blind to the `include:` graph (discovery is a filename glob
→ over-discovery), and the `sed`-based substitution produced an injection-class
bug (a `|`/`&` hostname crashed the engine — found and fixed).

The maintainer's drivers for moving off `sh`: **maintainability** (727-line
shell, e2e-only tests), **feature ceiling** (no real parser / data structures /
validation), and **distribution/UX** (want an installable CLI, not vendored
`.sh` per project). The fragile engine is a symptom, not the stated driver.

## 2. Decision summary

- **Language/engine: Go + `github.com/compose-spec/compose-go`** — the loader
  Docker Compose itself uses. It provides real YAML, full `${...}` interpolation,
  `include:`-graph resolution, `env_file` resolution, profiles, and merge — by
  import, eliminating the entire bug class above. This is the decisive factor.
- **Binary name: `cenvkit`.** Project keeps the name compose-envkit; the shipped
  command is `cenvkit`.
- **Distribution: dual-mode** — installable (`go install`, brew, GH-release
  binaries, `go run …@latest`) AND vendorable (commit the module + a POSIX shim
  that runs `go run ./cmd/cenvkit`). Vendored mode MAY require the Go toolchain
  (the maintainer accepted this), so no committed per-OS binaries and no
  first-run downloads are needed; both distribution modes are first-class.
- **v1 scope: "thin"** — assemble `COMPOSE_ENV_FILES` (now via compose-go's
  accurate model) and `exec docker compose`. Preserve the proven model; only the
  engine internals change. The "rich" mode (render a fully-resolved compose,
  native validation/provenance) is deferred (§11).
- **Repo: evolve the current `compose-envkit` repo** (not a new repo). The sh kit
  stays as legacy/reference during the transition; the existing
  `examples/monorepo/` + smoke suite become the Go tool's acceptance tests.
- **Upstream-first principle:** lean on compose-go / upstream Compose semantics;
  do not reimplement or diverge. Track compose-go versions; the tool is a thin,
  upstream-faithful layer over `docker compose`.
- **make:** the CLI replaces `make env-debug-*` as the env interface, but make
  (and `Taskfile`) remain the user's *task manager* — complementary, not removed
  (§7).

## 3. Architecture

```
compose-envkit/                  (existing repo, evolved)
├── go.mod / go.sum
├── cmd/cenvkit/                 CLI entry (cobra)
├── internal/chain/             Layer-1: .docker-env-chain parse + token substitution
│                                 (${ENV}/${COMPOSE_ENV}/${HOST}/${HOSTNAME}) — pure Go strings
├── internal/engine/            Layer-2 via compose-go: load project, enumerate the
│                                 ACTIVE set of resolved env_file paths; build COMPOSE_ENV_FILES
├── internal/debug/             env-debug modes over the loaded model (real provenance)
├── internal/bootstrap/         `cenvkit init` (port of the no-sudo/no-chmod init.sh)
├── examples/monorepo/          (kept) — now also the acceptance fixture
├── test/                       (kept sh smoke as acceptance) + Go unit/integration tests
└── lib/ mk/ bin/docker …       (legacy sh kit — retained during transition, then deprecated)
```

Each unit has one purpose and a testable interface: `chain` (files → ordered
Layer-1 list), `engine` (project dir + ENV → ordered `COMPOSE_ENV_FILES`),
`debug` (loaded model → human/inspection output), `bootstrap` (seed + fan-out).

## 4. Core algorithm (v1 "thin")

1. Resolve `ENV` (shell `COMPOSE_ENV` > `.env` `COMPOSE_ENV=` > `dev`) and
   `HOST` (exported `HOSTNAME` > `hostname` cmd; sanitized).
2. **Layer 1** — read `.docker-env-chain` (or built-in defaults
   `.env → .${ENV}.env → .secrets.env`), substitute tokens, keep existing files
   in order.
3. **Layer 2** — use compose-go to load the project honoring `COMPOSE_FILE` and
   `include:`. From the loaded model, enumerate each service's `env_file`
   entries as resolved absolute paths. This is the **active** set (no glob,
   include-aware), with real interpolation already applied to the paths. Dedup
   against Layer 1 (Layer 1 wins).
4. `export COMPOSE_ENV_FILES="<layer1>,<layer2>"`; `exec docker compose "$@"`.

Ordering note: Layer 1 must be visible to compose-go when it interpolates
`env_file` paths that reference earlier-chain vars (e.g. `${COMPOSE_ENV}`,
`${SVC_DIR}`); the engine seeds the load environment with the Layer-1 result
first. This preserves the M3 "rename via chain" behavior with upstream-correct
interpolation.

## 5. CLI surface

- `cenvkit compose <args>` — assemble chain, `exec docker compose` (the core).
- `cenvkit env-files` — print the resolved chain, one path/line.
- `cenvkit env-debug [--chain|--diff|--effective|--files|--trace VAR|--value VAR] [--service S]`
  — backed by the loaded model (real provenance, not re-derived).
- `cenvkit validate` — `docker compose config -q` for the active project (dev/prod).
- `cenvkit init` — port of the bootstrap (seed `.X` from `example.X` no-clobber,
  fan out to subproject inits) — no sudo/chmod/persisted secrets.
- `cenvkit version`.

Backward-compatible config: the existing `.docker-env-chain` format and the
`COMPOSE_ENV` / `COMPOSE_DEPTH` semantics are honored (with `COMPOSE_DEPTH`
likely obsolete once include-graph resolution replaces glob discovery — see §12).

## 6. Distribution (dual-mode)

- **Install:** `go install github.com/<org>/compose-envkit/cmd/cenvkit@latest`;
  a Homebrew tap; prebuilt binaries per-OS via goreleaser on GH releases;
  `go run …/cmd/cenvkit@latest …` for ephemeral (npx-like) use.
- **Vendor:** commit the Go module into the project + a small POSIX `./cenvkit`
  shim that execs `go run ./cmd/cenvkit "$@"` (or a pinned module path). Requires
  a Go toolchain in vendored mode (accepted). No committed binaries, no network.

## 7. make / task-runner integration

cenvkit owns the **env** layer; make/`task` own the **task** layer — they
compose, they don't compete. The kit therefore:

- Replaces `make env-debug-*` with `cenvkit env-debug …` (the .mk wrappers are
  retired as the *interface*).
- Ships an OPTIONAL thin delegation layer for whichever task runner the user has:
  a tiny `compose.mk` include and/or a `Taskfile.yml` snippet whose targets call
  `cenvkit` (e.g. `up: cenvkit compose up`). The kit does not require make.
- Future consideration (not v1): first-class [go-task](https://taskfile.dev)
  `Taskfile` integration / generation. Captured as a follow-up, not built now.

## 8. Repo & migration strategy

- **Evolve in place.** Add the Go module alongside the sh kit. The sh kit remains
  functional as legacy/reference until the Go tool reaches parity.
- **Acceptance via the existing suite.** Port `test/smoke-monorepo.sh` (61
  assertions) and `test/smoke.sh` to drive `cenvkit` instead of `./docker`. The
  Go tool is "done for v1" when it passes the same acceptance suite. The
  `examples/monorepo/` blueprint is the shared fixture.
- **Flip at parity, then deprecate sh.** Once green, `cenvkit` becomes the
  documented default; the sh `./docker`/`scripts/`/`lib/`/`mk/` are marked
  deprecated (kept one release for migrants, then removed).
- **Legacy monorepo cutover gets easier.** compose-go's real interpolation +
  include-graph awareness removes several blockers we documented in
  `docs/monorepo.md → Migrating an existing monorepo`: `${SVC_MONOREPO_DIR}`
  env_file pointers resolve, nested defaults resolve, yandex/stray composes are
  no longer over-discovered. The Go rewrite is also a migration enabler.

## 9. Testing & errors

- Go unit tests for `chain` (token substitution, ordering, missing-file skip),
  `engine` (Layer-2 enumeration over fixture projects incl. include + deep
  nesting), `debug` modes — table-driven, fast, no e2e dependency.
- Integration tests that invoke a real `docker compose` where available; the
  ported smoke suite as the cross-tool acceptance gate.
- Real Go errors with actionable messages (replacing silent `sh` failures);
  `--help` per subcommand via cobra.
- CI: `go test`, `go vet`, `golangci-lint`, goreleaser dry-run.

## 10. Upstream-fidelity policy

- The engine's source of truth is compose-go; behavior should match
  `docker compose` for the same inputs. Pin a compose-go version; bump
  deliberately and re-run the acceptance suite. Avoid local forks of compose
  semantics. Where upstream changes behavior, follow it.

## 11. Non-goals / deferred

- "Rich" mode (render a fully-resolved single compose; native `--effective`
  provenance beyond what env-debug needs) — deferred; revisit after v1.
- Plugin system; Terraform `TF_VAR_*` fan-out (stays out of scope — orthogonal
  tooling); pnpm/yarn wrappers.

## 12. Open / risk items

- **compose-go API stability** — it is a library with an evolving API; pin and
  isolate behind `internal/engine` so upgrades are localized.
- **`COMPOSE_DEPTH` fate** — likely obsolete (include-graph replaces glob); decide
  whether to drop it or keep as a no-op/back-compat alias.
- **Vendored mode needs Go** — accepted tradeoff; document clearly (vs the sh
  kit's zero-toolchain vendoring).
- **Version floor** — Docker Compose ≥ 2.24 already required; confirm the
  compose-go version that matches the targeted compose features.
- **Naming of internal config** — keep `.docker-env-chain` for back-compat or
  introduce `.cenvkit-chain`? Lean keep-for-compat in v1.

## 13. Acceptance criteria (v1 done)

1. `cenvkit` passes the ported `smoke-monorepo` (≈61 assertions) and `smoke`
   suites — behavior parity with the sh kit.
2. The four engine-ceiling bugs are structurally gone: `${SVC_DIR}` and nested
   `${...}` resolve; no glob over-discovery (include-aware); no sed-injection
   class (pure Go strings).
3. Both distribution modes work: `go install` + `go run @latest`, and vendored
   `./cenvkit` via `go run`.
4. Go unit tests cover `chain`/`engine`/`debug`; CI green.
