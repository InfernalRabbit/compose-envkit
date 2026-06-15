# cenvkit — Go rewrite design (compose-envkit → `cenvkit`)

Status: approved direction (brainstorming), 2026-06-15. Dry-run findings folded
2026-06-15 (sources: `.claude/artifacts/compose-go-api.md`, `spec-audit.md`,
`acceptance-port-plan.md`). One decision still **pending user confirmation: D1**
(§4). Next: implementation plan.

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

- **Language/engine: Go + `github.com/compose-spec/compose-go/v2`** — the loader
  Docker Compose itself uses. It provides real YAML, full `${...}` interpolation,
  `include:`-graph resolution, `env_file` resolution, profiles, and merge — by
  import, eliminating the entire bug class above. This is the decisive factor.
  **Pin `v2.11.0`** (D2; verified via `go doc` against real source — closes audit
  C2); use the **`/v2`** module (a transitive non-v2 v1.20.2 must not be imported).
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

**Engine seam (D3 — `internal/engine` is the ONLY package importing compose-go;
`debug`/`cmd`/`chain` stay compose-go-free):**

```go
package engine

type Input struct {
    ProjectDir  string   // absolute working dir
    ConfigFiles []string // explicit -f; empty => COMPOSE_FILE / default discovery
    Env         []string // Layer-1 chain result "K=V" — seeds interpolation (§4 note)
    Profiles    []string // active profiles (M3)
}
type Result struct {
    EnvFiles []string    // absolute, active services only, DETERMINISTICALLY ordered
    Project  ProjectView // compose-go-free projection for env-debug/provenance
}
type ProjectView struct {
    WorkingDir string
    Services   map[string][]string // service -> resolved env_file abs paths
}
type Engine interface { Resolve(ctx context.Context, in Input) (Result, error) }
func New() Engine // compose-go-backed, pinned to v2.11.0
```

`Result`/`Input`/`ProjectView` are plain Go (no compose-go types leak) so qa can
table-drive `Resolve` over `examples/monorepo/` fixtures and fake the `Engine`.

## 4. Core algorithm (v1 "thin")

1. Resolve `ENV` (shell `COMPOSE_ENV` > `.env` `COMPOSE_ENV=` > `dev`) and
   `HOST` (exported `HOSTNAME` > `hostname` cmd; sanitized).
2. **Layer 1** — read `.docker-env-chain` (or built-in defaults
   `.env → .${ENV}.env → .secrets.env`), substitute tokens, keep existing files
   in order.
3. **Layer 2** — load the project via compose-go and enumerate the **active**
   set of `env_file` paths. Concrete call (verified, closes audit C2):

   ```go
   opts, _ := cli.NewProjectOptions(in.ConfigFiles,
       cli.WithWorkingDirectory(in.ProjectDir),
       cli.WithConfigFileEnv,        // honor COMPOSE_FILE + resolve include: (the no-glob win)
       cli.WithDefaultConfigPath,    // default docker-compose.y*ml discovery when none given
       cli.WithEnv(in.Env),          // seed Layer-1 chain result for interpolation
       cli.WithProfiles(in.Profiles),
       cli.WithResolvedPaths(true),  // EnvFile.Path => ABSOLUTE
       cli.WithInterpolation(true))
   proj, err := opts.LoadProject(ctx)
   // iterate proj.Services (ACTIVE set; profile-off => proj.DisabledServices)
   //   -> svc.EnvFiles[].Path   (types.EnvFile{Path, Required OptOut, Format})
   ```

   `types.Project.Services` is already profile-filtered and include-merged, so
   iterating it gives the active env_file set with **no glob and no over-discovery**.
4. **Determinism:** `Services` is a Go map → the engine MUST sort (service name,
   then file order within a service) before emitting, so `COMPOSE_ENV_FILES` is
   stable (a contract qa pins).
5. `export COMPOSE_ENV_FILES="<layer1>,<layer2>"`; `exec docker compose "$@"`.
   The real `docker compose` run loads *again* with these files in the
   interpolation context — that second load is what makes a `${APP_PORT}` defined
   only in an `env_file:` resolve instead of falling back.

### 4a. Resolution model for `env_file:` paths (resolves audit C1)

compose-go interpolates an `env_file:` **path** using only the load environment
we seed via `WithEnv` (= the Layer-1 chain result), NOT values defined inside
*other* services' Layer-2 env_files. Therefore, for v1 "thin":

- **An `env_file:` path may reference only Layer-1 / project-chain vars**
  (`${COMPOSE_ENV}`, `${SVC_DIR}` defined in `.env`, …). This matches the legacy
  contract and the M3 "rename via chain" behavior, single-pass and
  upstream-faithful.
- A path that depends on a var defined **only inside another Layer-2 env_file** is
  **unsupported** in v1 (single pass cannot resolve it). Acceptance asserts this
  case is unsupported (errors / does not silently mis-resolve), not "magically
  works". A bounded two-pass fixpoint is **deferred** (§11).

### 4b. Missing `env_file:` behavior — D1 (⚠️ PROVISIONAL, pending user confirm)

`types.EnvFile.Required` is `OptOut` (**default true** upstream — a missing
*required* env_file makes `LoadProject` error). Parity tension: the sh kit
silently skips missing files. **Lead's lean (parity-preserving):**
**lenient at assembly, upstream at runtime** — the Layer-2 *enumeration* pass
skips a missing env_file (so chain assembly never aborts and the smoke suite's
missing-file-skip assertions stay green), while the actual `docker compose` exec
enforces `required:` exactly as upstream. **This is parity-affecting and must be
confirmed by the user before it is locked.**

### 4c. Precedence vs dedup (resolves audit W3)

"Layer 1 before Layer 2" is **dedup + ordering**, not a new precedence rule: a
path present in both layers is emitted once, in its Layer-1 position. Variable
**precedence is last-wins by file order** in `COMPOSE_ENV_FILES`; the emitted
order is `<Layer 1 in chain order (…, .secrets.env LAST)>, <Layer 2 in
deterministic order>`. Secrets stay last *within Layer 1*; acceptance asserts a
Layer-2 env_file does **not** clobber a secret var.

## 5. CLI surface

- `cenvkit compose <args>` — assemble chain, `exec docker compose` (the core).
- `cenvkit env-files` — print the resolved chain, one path/line.
- `cenvkit env-debug [--chain|--diff|--effective|--files|--trace VAR|--value VAR] [--service S]`
  — backed by the loaded model (real provenance, not re-derived).
- `cenvkit validate` — `docker compose config -q` (resolves S3): validates the
  **currently-resolved** `COMPOSE_ENV` by default; `--all` validates dev AND prod
  (matching legacy `make validate`). Non-zero exit on invalid config.
- `cenvkit init` — port of the bootstrap (seed `.X` from `example.X` **no-clobber**,
  fan out to subproject inits) — no sudo/chmod/persisted secrets. The no-clobber
  is guarded by acceptance (W5, §13).
- `cenvkit version`.

Backward-compatible config: the existing `.docker-env-chain` format and
`COMPOSE_ENV` are honored. **`COMPOSE_DEPTH` is resolved (was open, audit W2/G3):
accepted-but-ignored back-compat alias** — the include-graph load makes
depth-bounded glob discovery obsolete, but the var is tolerated (no error) so
existing setups/smoke assertions don't break. (task #2 greps the suites to
confirm none assert depth *behavior*.)

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
- **`chain` sanitization contract (audit W1):** "pure Go strings" kills the sed
  vector but Layer-1 still interpolates a host-derived value into paths.
  `chain` MUST whitelist the `HOST`/`ENV` charset and reject/escape a `,` (the
  `COMPOSE_ENV_FILES` separator) or path-traversal in any resolved path. Unit
  test must be **RED on a naive impl** (e.g. hostname `a,b` / `a|b`).
- **Error-behavior policy (audit S2):** missing chain files are **skipped
  silently** (parity); a malformed `.docker-env-chain` or a compose-go load
  failure is **fatal** with an actionable message. State per-case so impl doesn't
  drift.
- **`cenvkit init` no-clobber guard (audit W5):** an acceptance test runs `init`
  against a repo with an existing non-empty `.secrets.env` and asserts it is
  **byte-identical** after — RED against a clobbering impl (secret-wipe class).
- CI: `go test`, `go vet`, `golangci-lint`, goreleaser dry-run.

## 10. Upstream-fidelity policy

- The engine's source of truth is compose-go; behavior should match
  `docker compose` for the same inputs. **Pin `github.com/compose-spec/compose-go/v2
  v2.11.0`** (the floor matching the Compose ≥ 2.24 target; resolves S1); bump
  deliberately and re-run the acceptance suite. Avoid local forks of compose
  semantics. Where upstream changes behavior, follow it. compose-go's API shifts
  release-to-release (e.g. `ProjectFromOptions` is already deprecated for the
  `LoadProject` method) — this is exactly why it is isolated behind `internal/engine`.

## 11. Non-goals / deferred

- "Rich" mode (render a fully-resolved single compose; native `--effective`
  provenance beyond what env-debug needs) — deferred; revisit after v1.
- **Two-pass / fixpoint `env_file:`-path resolution** (a path referencing a
  Layer-2-only var) — deferred; v1 is single-pass, Layer-1-only (§4a).
- Plugin system; Terraform `TF_VAR_*` fan-out (stays out of scope — orthogonal
  tooling); pnpm/yarn wrappers.

## 12. Open / risk items

**Still open — needs the user:**
- **D1 (parity-affecting) — PENDING USER CONFIRM.** Missing *required* `env_file:`:
  lenient-skip at Layer-2 enumeration vs upstream-fatal. Lead's lean = lenient at
  assembly / upstream at runtime (§4b). Confirm before locking.

**Resolved by the dry-run (kept here for traceability):**
- ~~compose-go API stability~~ → pinned **v2.11.0**, isolated behind
  `internal/engine` (§3 seam, §10); concrete call path cited (§4, closes C2).
- ~~`COMPOSE_DEPTH` fate~~ → **accepted-but-ignored back-compat alias** (§5; W2/G3).
- ~~Version floor~~ → **v2.11.0** matches Compose ≥ 2.24 (§10; S1).
- ~~C1 circular interpolation~~ → resolution model fixed: single-pass,
  Layer-1-only path refs (§4a); two-pass deferred (§11).

**Accepted trade-offs / minor:**
- **Vendored mode needs Go + per-invocation `go run`** (build-cache) latency vs
  the sh kit's zero-toolchain vendoring (audit W4) — accepted; recommend
  documenting `go build` into a gitignored local binary as the faster vendored
  path (still no committed binary).
- **Internal config naming** — keep `.docker-env-chain` for back-compat in v1
  (not `.cenvkit-chain`).

## 13. Acceptance criteria (v1 done)

1. `cenvkit` passes the ported `smoke-monorepo` (**61** assertions — exact, S4)
   and `smoke` suites — behavior parity with the sh kit, with these **deliberate
   inversions** the port must encode (audit G1–G5, plan in
   `.claude/artifacts/acceptance-port-plan.md`):
   - **G1/G2 (scenarios 9, 10):** over-discovery + `docker-compose*.yml`-glob
     assertions **invert** — compose-go's include-graph eliminates those sh
     quirks; the ported assertions verify the *correct* (no-over-discovery)
     behavior, not the old quirk.
   - **G3 (scenario 11):** `COMPOSE_DEPTH` is accepted-but-ignored (§5).
   - **G4 (scenario 14):** the inline fallback-shim scenario has **no Go
     equivalent** (the binary IS the engine) — rewrite as "chain-only when no
     compose file present".
   - **G5:** confirm the final install-artifact set for the `smoke.sh` layout
     checks (cenvkit binary/shim vs the sh `scripts/` layout).
2. Engine-ceiling bugs structurally gone: `${SVC_DIR}` + nested `${...}` resolve;
   no glob over-discovery (include-aware); no sed-injection class (pure Go).
3. **New guard tests, each RED on a pre-fix/naive impl:**
   - W1 — a hostname `a,b` / `a|b` does not split/inject a `COMPOSE_ENV_FILES` entry.
   - W3 — a Layer-2 `env_file:` does NOT clobber a secret var (`.secrets.env` last).
   - W5 — `cenvkit init` leaves an existing non-empty `.secrets.env` byte-identical.
   - C1 — an `env_file:` *path* referencing a Layer-2-only var is unsupported
     (errors / no silent mis-resolve), not magically resolved.
   - D1 — missing `env_file:` behavior matches the confirmed ruling (§4b).
4. Both distribution modes work: `go install` + `go run @latest`, and vendored
   `./cenvkit` via `go run`.
5. Go unit tests cover `chain`/`engine`/`debug` (table-driven, `Engine` faked);
   `go vet` + `gofmt` + `golangci-lint` clean; CI green.
