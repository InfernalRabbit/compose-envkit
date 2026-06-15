# Acceptance-port plan: smoke suites → `cenvkit`

Task #2 (qa-engineer). PLAN ONLY — no Go test code written.
Date: 2026-06-15. Blocked until: task #1 completed (go-eng). Interface sketch
received via DM 2026-06-15; full API in `.claude/artifacts/compose-go-api.md §3`.

---

## 0. Ground rules for the port

**Source of truth:** `test/smoke-monorepo.sh` (23 scenarios, **61** assertions —
the suite self-reports `passed: 61` when run, and the per-scenario PASS tally
sums to 61) + `test/smoke.sh` (7 sections). After the S4 deltas (drop 11.2;
scenarios 9/10/22 are count-neutral polarity flips) the ported `smoke-monorepo`
target is **60** assertions. These run against the sh kit's `./docker` shim.
The port replaces every `./docker` and `run_shim` invocation with `cenvkit`
subcommands while keeping the assertion logic unchanged. The Go tool is
"v1 done" when ported suites stay green (spec §8 / §13).

**Invocation map (the core substitution):**

| sh kit call | cenvkit call |
|---|---|
| `./docker compose config` | `cenvkit compose config` |
| `./docker compose config --services` | `cenvkit compose config --services` |
| `./docker env-files` | `cenvkit env-files` |
| `sh scripts/env-debug.sh --chain` | `cenvkit env-debug --chain` |
| `sh scripts/env-debug.sh --diff` | `cenvkit env-debug --diff` |
| `sh scripts/env-debug.sh --effective` | `cenvkit env-debug --effective` |
| `sh scripts/env-debug.sh --files` | `cenvkit env-debug --files` |
| `sh scripts/env-debug.sh --trace --var V` | `cenvkit env-debug --trace --var V` |
| `sh scripts/env-debug.sh --value --var V` | `cenvkit env-debug --value --var V` |
| `make env-debug` | `cenvkit env-debug` |
| `make env-debug-diff` | `cenvkit env-debug --diff` |
| `make env-debug-effective` | `cenvkit env-debug --effective` |
| `make env-debug-files` | `cenvkit env-debug --files` |
| `make env-debug-trace VAR=X` | `cenvkit env-debug --trace --var X` |
| `make validate` | `cenvkit validate` |

**Fixture:** `examples/monorepo/` is the shared fixture for both the sh suite
and the Go acceptance tests. No fixture changes anticipated; the port drives the
same directory tree.

**Engine-dependent** (marked **E** below): assertions whose pass/fail is
determined by `internal/engine.Engine.Resolve` — i.e., they require compose-go
to load the project, resolve include:, enumerate active env_files, or enforce
required-missing behavior. These cannot be unit-tested in `internal/chain`
alone and must hit the real engine (or a faked `Engine` interface).

**Chain-only** (marked **C**): assertions exercising Layer-1 only
(`.docker-env-chain` parse, token substitution, file-existence filter). These
can be tested in `internal/chain` without compose-go.

**Install/CLI-wiring** (marked **W**): assertions about install layout,
`cenvkit init`, shim executability, or CLI plumbing (no engine or chain logic).

**D1 sensitivity** (marked **D1**): assertions that could flip depending on the
lead's ruling on required-missing env_file behavior (fatal vs. skip).

---

## 1. `test/smoke-monorepo.sh` — assertion map (23 scenarios, 61 assertions baseline)

### [1] Copy blueprint + install (3 assertions)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 1.1 | blueprint files copied | W | — (setup step, no cenvkit) | unchanged |
| 1.2 | `install.sh` exits 0 | W | `cenvkit init` (replaces install.sh for Go mode) | see §4 gap |
| 1.3 | `example.env` not clobbered | W | `cenvkit init` no-clobber | `init` must be idempotent |

Also verifies presence of: `docker`, `scripts/compose-env.sh`,
`scripts/parse-compose-env-files.sh`, `.docker-env-chain`, `web/docker-compose.yml`,
`web/.web.env`, `api/docker-compose.yml`, `api/.api.env`. For the Go port these
become: `cenvkit` binary present + `.cenvkit-chain` or `.docker-env-chain` (back-compat).

### [2] Native baseline — gap proof (1 assertion, docker-dependent)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 2.1 | raw `docker compose config` shows :-0 fallback for BOTH ports | W | `docker compose config` (NOT cenvkit) | proves the gap; no substitution needed |

This scenario is a *negative* control: it runs native docker without cenvkit.
Unchanged in the port — still call `docker compose config` directly.

### [3] ROOT + kit: cross-subproject Layer-2 (3 assertions, docker-dependent)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 3.1 | WEB_PORT == 18080 | **E** | `cenvkit compose config` | engine must enumerate web/.web.env |
| 3.2 | API_PORT == 19090 | **E** | `cenvkit compose config` | engine must enumerate api/.api.env |
| 3.3 | no published port == :-0 | **E** | `cenvkit compose config` | negative: no fallback |

Depends on `Engine.Resolve` cross-subproject behavior: `Input.ConfigFiles` empty
(root dir), `Result.EnvFiles` must contain both `web/.web.env` and `api/.api.env`.
Assert `Result.EnvFiles` slice contains both paths.

### [4] env-files discovery from root (3 assertions, no docker)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 4.1 | web/.web.env in output | **E** | `cenvkit env-files` | engine Layer-2 cross-subproject |
| 4.2 | api/.api.env in output | **E** | `cenvkit env-files` | engine Layer-2 cross-subproject |
| 4.3 | root .env in output | **C** | `cenvkit env-files` | Layer-1 must always list root .env |

`cenvkit env-files` prints `Result.EnvFiles` (Layer-1 + Layer-2 merged). 4.3 is
chain-only; 4.1/4.2 depend on the engine.

### [5] Isolated web/ (Option A shim) (2 assertions; 1 docker-dependent)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 5.1 | web/ env-files lists web/.web.env | **E** | `cenvkit env-files` (cwd=web/) | engine scoped to web/ |
| 5.2 | isolated web/ WEB_PORT == 18080 | **E** | `cenvkit compose config` (cwd=web/) | docker-dependent |

In the Go port, "drop a copy of ./docker into web/" → "invoke cenvkit with
`--project-dir web/`" or `cwd=web/`. Equivalent: `cenvkit --project-dir web/
env-files`. The invocation must scope the engine's `Input.ProjectDir` to the
subdir.

### [6] Isolated api/ (Option A shim) (3 assertions; 1 docker-dependent)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 6.1 | api/ env-files lists api/.api.env | **E** | `cenvkit env-files` (cwd=api/) | engine scoped to api/ |
| 6.2 | api/ does NOT see web/.web.env | **E** | `cenvkit env-files` (cwd=api/) | **negative**: sibling isolation |
| 6.3 | isolated api/ API_PORT == 19090 | **E** | `cenvkit compose config` (cwd=api/) | docker-dependent |

6.2 is a critical negative assertion: the engine, when scoped to `api/`, must
NOT include web/.web.env in `Result.EnvFiles`. This validates that
`Engine.Resolve` is driven by project dir, not a global glob.

### [7] Option B — standalone subproject (3 assertions; 1 docker-dependent)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 7.1 | install.sh into standalone exits 0 | W | `cenvkit init` into an isolated dir | Go port uses `cenvkit init` |
| 7.2 | standalone has own scripts/ + ./docker | W | check binary present | install contract |
| 7.3 | standalone discovers .web.env (Layer-2) | **E** | `cenvkit env-files` (cwd=ISO) | engine self-contained |
| 7.4 | standalone WEB_PORT == 18080 | **E** | `cenvkit compose config` (cwd=ISO) | docker-dependent |

### [8] COMPOSE_ENV=prod (2 assertions; 1 docker-dependent)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 8.1 | prod: Layer-2 discovers web/.web.env AND api/.api.env | **E** | `COMPOSE_ENV=prod cenvkit env-files` | engine must not break under env switch |
| 8.2 | prod: both ports still resolve | **E** | `COMPOSE_ENV=prod cenvkit compose config` | docker-dependent |

### [9] Over-discovery of stray compose (1 assertion, no docker)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 9.1 | stray extra/docker-compose-extra.yml's .extra.env IS discovered | **E** | `cenvkit env-files` | |

**BEHAVIOR CHANGE WARNING.** The sh kit over-discovers by filename glob. The Go
engine uses compose-go's include-graph — a stray compose file NOT in `include:`
or `COMPOSE_FILE` is *not* loaded. This assertion documents a sh-kit quirk
("over-discovery — documented", smoke-monorepo.sh:464). In the Go port the
assertion will likely **invert**: the stray file is NOT discovered (the bug is
gone). The ported suite should assert the Go behavior (stray NOT discovered)
and update the scenario label to "over-discovery eliminated". Flag this to the
lead before writing the test — it is a documented behavior change, not a bug.

### [10] Glob limit (2 assertions, no docker)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 10.1 | compose.yaml is NOT discovered | **E** | `cenvkit env-files` | |
| 10.2 | renamed docker-compose.yml IS discovered | **E** | `cenvkit env-files` | |

**BEHAVIOR CHANGE WARNING.** The sh kit's glob only matches `docker-compose*.yml`.
compose-go's default discovery matches `compose.yaml`, `compose.yml`,
`docker-compose.yml`, `docker-compose.yaml` (all standard names). So 10.1 may
**invert** in the Go port: `compose.yaml` IS found (compose-go matches it). The
ported assertion should verify that compose-go's standard discovery picks up all
canonical filenames. Flag to lead.

### [11] COMPOSE_DEPTH boundary (2 → 1 assertion; 11.2 DROPPED, no docker)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 11.1 | depth-4 compose missed at default (out-of-include = not-found) | **E** | `cenvkit env-files` | keep/reframe |
| ~~11.2~~ | ~~depth-4 found with COMPOSE_DEPTH=4~~ | — | — | **DROPPED (S4 −1):** `a/b/c/docker-compose.yml` is never in the root `include:`, so `.deep.env` is never enumerated regardless of `COMPOSE_DEPTH`; the knob is a no-op (accepted-but-ignored) |

**BEHAVIOR CHANGE WARNING.** `COMPOSE_DEPTH` controls the sh kit's find-by-glob
depth. With compose-go's include-graph resolution, depth is implicit (the loader
follows `include:` links wherever they go). `COMPOSE_DEPTH` may become a no-op.
If so: 11.1 passes trivially (depth-4 file not in include: is not found) and
11.2 cannot be tested as written. This is D2 in the spec §12: "COMPOSE_DEPTH
likely obsolete." Flag to lead before porting; plan may be to drop these two
assertions or reframe as "not-in-include means not-found" (compose-go behavior).

### [12] Host overrides (4 assertions, no docker)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 12.1 | .testhost.env discovered via ${HOSTNAME} | **C** | `HOSTNAME=testhost cenvkit env-files` | chain token substitution |
| 12.2 | chain order .env < .testhost.env < .secrets.env | **C** | `HOSTNAME=testhost cenvkit env-files` | Layer-1 ordering |
| 12.3 | non-matching hostname: .testhost.env NOT discovered | **C** | `HOSTNAME=otherhost cenvkit env-files` | negative; chain-only |
| 12.4 | (implicit) secrets file stays last | **C** | same | chain ordering invariant |

All four are pure `internal/chain` — no compose-go needed. `cenvkit env-files`
output is the full merged list; position of files can be asserted by index.

### [13] ${HOST} token (1 assertion, no docker)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 13.1 | ${HOST} substitutes same as ${HOSTNAME} | **C** | `HOSTNAME=testhost cenvkit env-files` (cwd=htest/) | chain token — both tokens must work |

Chain unit test: parse a `.docker-env-chain` containing `.${HOST}.env`, assert
resolved path == `.testhost.env` with `HOSTNAME=testhost`.

### [14] Fallback shim host-token substitution (1 assertion, no docker)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 14.1 | fallback shim substitutes ${HOSTNAME} | **C** | `HOSTNAME=testhost cenvkit env-files` (cwd=FBK/) | fallback chain behavior |

In the Go port there is no "fallback shim" separate binary — `cenvkit` always
runs the same engine. The scenario collapses: a project with only a
`.docker-env-chain` and no compose file resolves its chain the same way. Assert
that `cenvkit env-files` in a dir with no compose file still processes the chain
file and substitutes tokens. This may require the engine to handle a
no-compose-file case gracefully (returns empty Layer-2, only Layer-1 listed).
Flag to lead if `cenvkit env-files` on a dir with no compose file should error
or succeed-with-chain-only.

### [15] dev/prod overlay via COMPOSE_FILE ${COMPOSE_ENV} (2 assertions, docker-dependent)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 15.1 | dev: STACK_TIER=dev from overlay | **E** | `cenvkit compose config` | engine loads compose file graph with env |
| 15.2 | prod: STACK_TIER=prod from overlay | **E** | `COMPOSE_ENV=prod cenvkit compose config` | |

Engine must seed `Input.Env` with Layer-1 so `COMPOSE_FILE` token
`docker-compose.${COMPOSE_ENV}.yml` resolves before compose-go loads the graph.

### [16] Per-service env tier (2 assertions, no docker)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 16.1 | dev: web/.web.dev.env discovered | **E** | `cenvkit env-files` | engine resolves ${COMPOSE_ENV} in env_file paths |
| 16.2 | prod: web/.web.prod.env found AND .web.dev.env excluded | **E** | `COMPOSE_ENV=prod cenvkit env-files` | engine + chain |

16.2 is both positive and negative — a compound assertion. Test both sides.

### [17] Root per-env tier (4 assertions; 1 docker-dependent)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 17.1 | dev: .dev.env in chain, .prod.env not | **C** | `cenvkit env-files` | chain token ${ENV} |
| 17.2 | prod: .prod.env in chain, .dev.env not | **C** | `COMPOSE_ENV=prod cenvkit env-files` | chain token |
| 17.3 | IS_DEV=true in rendered dev config | **E** | `cenvkit compose config` | docker-dependent, engine feeds IS_DEV |
| 17.4 | IS_DEV=false in rendered prod config | **E** | `COMPOSE_ENV=prod cenvkit compose config` | docker-dependent |

### [18] Profiles passthrough (3 assertions, docker-dependent)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 18.1 | profiled 'tools' service OFF by default | **E** | `cenvkit compose config --services` | engine DisabledServices |
| 18.2 | web + api active by default | **E** | `cenvkit compose config --services` | engine active set |
| 18.3 | COMPOSE_PROFILES=tools enables 'tools' | **E** | `COMPOSE_PROFILES=tools cenvkit compose config --services` | `Input.Profiles` passthrough |

Profiles are a key contract seam: `Input.Profiles` feeds `cli.WithProfiles` →
`Project.DisabledServices` excludes inactive services from `Result.EnvFiles`.
18.1 also acts as the env-file-set gate: a profiled service's env_files must
NOT appear in `Result.EnvFiles` when the profile is inactive.

### [19] Namespacing (1 assertion, docker-dependent)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 19.1 | .nsvc.env renames SITE_URL→NSVC_SITE via chain | **E** | `cenvkit compose config` (cwd=nstest/) | Layer-1 before Layer-2 ordering |

Tests the Layer-1-before-Layer-2 contract: `SITE_URL` from root `.env` (Layer-1)
must be in `Input.Env` when the engine loads so `NSVC_SITE=${SITE_URL:-fallback}`
in `.nsvc.env` resolves correctly. This is the contract-seam assertion between
chain output → engine input.

### [20] Bootstrap init.sh (5 assertions)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 20.1 | init.sh generated + executable | W | `cenvkit init` generates an executable | |
| 20.2 | pre-existing .env not clobbered | W | `cenvkit init` no-clobber | |
| 20.3 | .dev.env + .prod.env seeded | W | `cenvkit init` seeds example.* | |
| 20.4 | sub/init.sh fanned out | W | `cenvkit init` fan-out to subdirs | |
| 20.5 | init.sh re-run idempotent | W | `cenvkit init` idempotent | |

All wiring/install assertions. The `cenvkit init` command replaces `sh init.sh`.
These are independent of the engine interface.

### [21] Deep nesting services/<svc>/ (2 assertions; 1 docker-dependent)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 21.1 | services/reports/.reports.env discovered from root | **E** | `cenvkit env-files` | engine include-graph reaches depth 3 |
| 21.2 | REPORTS_PORT=15151 resolved | **E** | `cenvkit compose config` | docker-dependent |

### [22] Submodule shape (2 assertions) — INVERTED (G1/G2 over-discovery class)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 22.1 | .git gitlink subproject NOT discovered | **E** | `cenvkit env-files` | INVERSION: non-included subproject not enumerated |
| 22.2 | .git directory subproject NOT discovered | **E** | `cenvkit env-files` | same — `.git` distinction moot under include-graph |

**BEHAVIOR CHANGE — RECLASSIFIED to the G1/G2 over-discovery inversion class.**
The legacy suite creates `vendored/` and `vendored2/` subprojects at test time
that the root `docker-compose.yml` does **NOT** `include:` (the include block is
exactly `./web`, `./api`, `./services/reports/` — `examples/monorepo/docker-compose.yml:20-23`).
The sh kit finds them by find-by-glob, blind to `.git`; compose-go's include-graph
never enumerates a non-included subproject's env_file. So `.vend.env`/`.vend2.env`
are **ABSENT** from `Result.EnvFiles`. The `.git` gitlink (22.1) vs real `.git`
directory (22.2) distinction is moot under the include-graph (no `.git` pruning
exists), so both collapse to negative assertions. This is the SAME class as
scenario 9 — assert the include graph, never the glob list. (Polarity flip,
count-neutral — see §4 G1.)

### [23] Host token sanitization (2 assertions)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 23.1 | engine survives HOSTNAME with sed-special chars (exit 0) | **C** | `HOSTNAME='ev|l&host' cenvkit env-files` | chain sanitization (no sed in Go) |
| 23.2 | sanitized host resolves .evlhost.env | **C** | same | [A-Za-z0-9._-] strip |

In Go there is no sed — but the chain must still sanitize the hostname to
`[A-Za-z0-9._-]` before path construction (otherwise arbitrary filenames).
This is a pure chain unit test: feed `HOSTNAME='ev|l&host'` → assert resolved
token == `evlhost`.

---

## 2. `test/smoke.sh` — assertion map (7 sections)

### [1] Project scaffold (1 assertion)

Setup only — creates a temp project. Unchanged.

### [2] Install (4 assertions)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 2.1 | install.sh exits 0 | W | `cenvkit init` | |
| 2.2 | vendored file layout present | W | binary + config file check | layout changes for Go (no scripts/) |
| 2.3 | ./docker is executable | W | `cenvkit` binary is executable | |
| 2.4 | re-install preserves existing .env | W | `cenvkit init` idempotent | |

Layout assertion (2.2) must be updated: the Go port removes `scripts/compose-env.sh`,
`scripts/parse-compose-env-files.sh`, `scripts/env-debug.sh`, `scripts/compose.mk`,
`.docker-env-chain` (maybe → `.cenvkit-chain`). Assert the Go port's layout
instead (binary + config file). Coordinate with go-eng on final install layout.

### [3] Layer-2 acceptance check (2 assertions, docker-dependent)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 3.1 | `SVC_PORT_VALUE` in compose config | **E** | `cenvkit compose config` | core Layer-2 check |
| 3.2 | published != :-0 fallback | **E** | `cenvkit compose config` | negative |

### [4] env-files listing (2 assertions)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 4.1 | svc.env in output | **E** | `cenvkit env-files` | Layer-2 discovery |
| 4.2 | .env in output | **C** | `cenvkit env-files` | Layer-1 |

### [5] env-debug modes (8 assertions)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 5.1 | --chain exits 0 | W/E | `cenvkit env-debug --chain` | docker-dependent |
| 5.2 | --diff exits 0 | W/E | `cenvkit env-debug --diff` | docker-dependent |
| 5.3 | --effective exits 0 | W/E | `cenvkit env-debug --effective` | docker-dependent |
| 5.4 | --files exits 0 | W/E | `cenvkit env-debug --files` | docker-dependent |
| 5.5 | --trace --var SVC_PORT exits 0 | W/E | `cenvkit env-debug --trace --var SVC_PORT` | docker-dependent |
| 5.6 | --value --var SMOKE_VAL == 'hello-layer1' | **C** | `cenvkit env-debug --value --var SMOKE_VAL` | no docker needed |
| 5.7 | --value on unset var yields empty | **C** | `cenvkit env-debug --value --var DEFINITELY_UNSET` | no crash under missing var |
| 5.8 | (implicit) no panic/crash on any mode | W | all debug modes | exit 0 + non-empty output |

### [6] make targets (6 assertions, make+docker-dependent)

In the Go port, make targets are thin wrappers calling `cenvkit`. The assertions
become: `make <target>` calls `cenvkit <subcommand>` and exits 0. These are
wiring tests, not engine tests. Can be skipped if make is absent (matches
existing smoke.sh behavior).

### [7] Subdir run (2 assertions; 1 docker-dependent)

| # | Assertion | Type | cenvkit invocation | Notes |
|---|---|---|---|---|
| 7.1 | subdir env-files resolves sub/svc.env | **E** | `cenvkit env-files` (cwd=sub/) or `--project-dir sub/` | |
| 7.2 | subdir Layer-2 check (SVC_PORT) | **E** | `cenvkit compose config` (cwd=sub/) | docker-dependent |

---

## 3. Engine-interface dependency map

The following assertions **must not be written as unit tests against
`internal/engine` alone** — they require `Engine.Resolve` (with the real or
faked compose-go backend) or the full `cenvkit compose config` CLI:

- Cross-subproject Layer-2: [3], [4.1–4.2], [5], [6.1/6.3], [7.3–7.4], [8], [15–19], [21], [22]
- Profile-gating of env_files: [18]
- Layer-1-before-Layer-2 ordering (contract seam): [19]

These are the tests that MUST wait for `internal/engine` to be implemented.

**Contract-seam tests** (testing both sides of the chain→engine interface):
- `chain` output (ordered `[]string` of `"K=V"`) → becomes `engine.Input.Env`
- `engine.Result.EnvFiles` (ordered absolute paths) → becomes the second half of `COMPOSE_ENV_FILES`
- A green unit test on each side does NOT catch drift between them — one
  dedicated seam test must pass `chain.Resolve()` output directly into a real
  `engine.Resolve()` call and assert the merged `COMPOSE_ENV_FILES` ordering.

**Fakeable via interface:** engine-dependent tests that run without docker can
use a fake `Engine` returning a fixture `Result`. Tests that assert actual
variable resolution (WEB_PORT, API_PORT, SVC_PORT) must run against real docker
compose and are gated by `SMOKE_SKIP_DOCKER`.

---

## 4. Gaps identified

### G1: Over-discovery inversion (scenarios 9, 10, 22)

The sh kit asserts stray files ARE discovered (glob-based). compose-go
include-graph means they will NOT be discovered. The ported suite must flip
these assertions. **Action:** flag to lead before writing tests; do not silently
change assertion polarity.

**Scenario 22 (submodule shape) belongs to this class.** Its `vendored/`/`vendored2/`
subprojects are NOT in the root `include:` list (verified: include is exactly
`./web`, `./api`, `./services/reports/`), so they are NOT discovered under the
include-graph — the `.git` gitlink-vs-directory distinction is moot (no `.git`
pruning exists). Both 22.1 and 22.2 invert to negatives (polarity flip,
count-neutral). **Process guard:** audit every test-time-created subproject
against the fixture's `include:` set; any subproject outside it must assert
"not discovered."

### G2: Glob name matching (scenario 10)

The sh kit only matched `docker-compose*.yml`. compose-go matches all standard
names including `compose.yaml`. 10.1's "compose.yaml is missed" may flip.
**Action:** same as G1 — flag to lead.

### G3: COMPOSE_DEPTH obsolescence (scenario 11)

`COMPOSE_DEPTH` is a sh-kit knob for find-depth. With include-graph it is likely
a no-op. **Action:** lead decides whether to drop COMPOSE_DEPTH assertions or
rewrite as "out-of-include = not-found" cases.

### G4: Fallback-shim scenario (scenario 14)

The "no scripts/ → inline fallback" path does not exist in Go. The scenario must
be rewritten as "project dir with no compose file: chain still processes,
Layer-2 returns empty." **Action:** confirm with go-eng whether `cenvkit
env-files` on a dir without a compose file is a valid (chain-only) or error
case.

### G5: Install layout assertions (smoke.sh [2])

The Go install layout differs from sh (no `scripts/*.sh`, no `scripts/*.mk`).
The ported install assertions must list the Go layout. **Action:** go-eng to
confirm the final install artifact set before these are written.

### G6: D1 — required-missing env_file behavior

go-eng flagged: sh kit silently skips a missing env_file; compose-go (with
`EnvFile.Required` defaulting true) errors. This could cause scenarios with
partially-missing fixtures to fail/error instead of skip. **Action:** lead rules
on D1 before porting any scenario that touches missing env_files (primarily
scenario 7, option B with a partially-missing fixture).

### G7: One real docker compose e2e

The spec requires at least one fast e2e against real docker compose (spec §9:
"catches wiring bugs unit tests miss"). The smoke suite provides this when
`SMOKE_SKIP_DOCKER=0`. The ported suite should keep the docker-dependent
assertions (marked E above) and run them in CI where docker is available.
The `SMOKE_SKIP_DOCKER=1` skip path must remain for environments without docker.

---

## 5. Porting strategy (phase ordering)

**Phase 1 — chain unit tests (no engine):** scenarios 12, 13, 14 (rewritten),
17.1–17.2, 23 + smoke.sh 5.6–5.7. These are pure `internal/chain` and can be
written immediately once `internal/chain` is coded.

**Phase 2 — engine unit/fixture tests (no docker):** scenarios 4, 6.1–6.2, 8.1,
16, 18.1 (env-files only), 21.1, 22. Drive `engine.Resolve()` against
`examples/monorepo/` fixtures with `SMOKE_SKIP_DOCKER=1`. Pin exact
`Result.EnvFiles` slices (sorted by service name then file order per
go-eng's contract).

**Phase 3 — contract-seam tests:** one test that passes `chain.Resolve()` output
directly into `engine.Resolve()` `Input.Env` and asserts the merged
`COMPOSE_ENV_FILES` ordering. Run against `examples/monorepo/`.

**Phase 4 — CLI + docker e2e (full smoke port):** scenarios 3, 5.2, 6.3, 7.4,
8.2, 15, 17.3–17.4, 18.2–18.3, 19, 21.2 + smoke.sh [3], [7.2]. These require
real docker and run only in `HAVE_DOCKER` mode. Keep `SMOKE_SKIP_DOCKER`
fallback. Must stay fast (<60s total per spec §9).

**Phase 5 — gap resolutions:** once lead rules on D1, G1–G6, update/add the
flagged assertions.

---

## 6. Summary counts

| Suite | Total scenarios | Total assertions | Engine-dependent (E) | Chain-only (C) | Wiring (W) |
|---|---|---|---|---|---|
| smoke-monorepo.sh | 23 | 61 baseline → **60** ported (drop 11.2) | ~38 | ~13 | ~10 |
| smoke.sh | 7 | ~25 | ~12 | ~4 | ~9 |

> The smoke-monorepo baseline is **61** (verified: the suite self-reports
> `passed: 61`; the per-scenario PASS tally sums to 61). Ported target after the
> S4 deltas (9/10/22 polarity flips = count-neutral; drop 11.2 = −1) is **60** —
> pinned in spec §13.1 and plan Task 7 Step 6, **lead signs off**.

Gaps requiring lead decision before porting: **G1 (§9), G2 (§10), G3 (§11),
G4 (§14), G5 (install layout), G6 (D1 behavior boundary).**
