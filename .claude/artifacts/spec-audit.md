# Spec audit — cenvkit Go rewrite design

Audited: `docs/superpowers/specs/2026-06-15-cenvkit-go-rewrite-design.md`
Reviewer: code-reviewer. Read-only. Date: 2026-06-15.
Cross-checked against legacy reference: `lib/compose-env.sh`,
`lib/parse-compose-env-files.sh`, `docs/concepts.md`, `docs/monorepo.md`.

All spec quotes below are literal (grep-verified). Findings are ordered
Critical / Warnings / Suggestions; each has a §-reference and a concrete fix.

---

## Critical

### C1 — The Layer-2 algorithm has a circular dependency the spec does not resolve

§4 step 3:
> "use compose-go to load the project honoring `COMPOSE_FILE` and `include:`.
> From the loaded model, enumerate each service's `env_file` entries as resolved
> absolute paths."

§4 ordering note:
> "Layer 1 must be visible to compose-go when it interpolates `env_file` paths
> that reference earlier-chain vars … the engine seeds the load environment with
> the Layer-1 result first."

The whole reason this tool exists (`docs/concepts.md:107-133`) is that a value
living only in a service `env_file:` is **not** in the interpolation context:
> "A service's **`env_file:`** populates only the **container's runtime
> environment**. It is **not** read into the interpolation context."

So the v1 flow is: load project → read env_file paths → add them to
`COMPOSE_ENV_FILES` → `exec docker compose` (which loads *again*, this time with
those files in the interpolation context). That second load is what makes
`${APP_PORT}` resolve to the env_file value instead of the `:-3000` fallback.

The gap: the spec's first load (engine enumeration) seeds the environment with
**Layer-1 only**, not Layer-2. If a service's `env_file:` *path* itself contains
a `${VAR}` whose value lives in **another service's env_file** (a Layer-2 value),
that path will not resolve on the enumeration pass — the exact class the tool
claims to fix. The legacy kit sidesteps this because its awk parser
(`lib/parse-compose-env-files.sh:53-54`) only ever substitutes `${COMPOSE_ENV}` /
`${COMPOSE_ENV:-default}` into env_file paths — it never depends on Layer-2
values to *find* Layer-2 files. The Go design, by routing discovery through a
real loader, newly introduces the possibility of a Layer-2-needs-Layer-2 cycle
and does not say how it terminates.

**Fix:** State the resolution model explicitly. Either (a) declare that env_file
**paths** may only reference Layer-1 / project-chain vars (matching the legacy
contract — paths resolve from Layer-1, *values* resolve on the second
`docker compose` load) and add an acceptance assertion that a Layer-2 var used
inside an env_file *path* is unsupported/errors; or (b) define a fixpoint/two-pass
enumeration and bound its iterations. Pick one in the spec before
`internal/engine` is built — this is the contract `engine` exposes to `chain` and
to `docker compose`.

### C2 — "resolved absolute paths … with real interpolation already applied" is an unverified compose-go API claim and is the engine's entire contract

§4 step 3:
> "enumerate each service's `env_file` entries as resolved absolute paths. This
> is the **active** set (no glob, include-aware), with real interpolation already
> applied to the paths."

§13 acceptance #2:
> "no glob over-discovery (include-aware)"

The design's decisive factor (§2) is that compose-go gives "real YAML, full
`${...}` interpolation, `include:`-graph resolution, `env_file` resolution … by
import." But the spec never cites the specific compose-go type/field that yields
the **post-include, post-interpolation, absolute** env_file list, nor whether
compose-go exposes env_file entries *before* it has consumed them into the
service environment (some loader versions fold env_file into the service `env`
map and may not retain the originating file list in a stable public field). If
that field is not stably exposed, the "no glob" guarantee collapses back toward
the legacy approach. Per CLAUDE.md "Upstream-first" + TEAM.md "any claim about a
helper/API/contract … cites a `file:line` you actually opened," this assertion
must be pinned to an actual compose-go symbol.

**Fix:** Block `internal/engine` on task #1 producing the concrete compose-go
call path (loader options, the type holding env_file entries, and the version
where it is stable) and fold that `pkg.Type.Field` reference into §4/§12. Do not
let `engine` be built against an assumed API.

---

## Warnings

### W1 — Layer-1 token substitution is reintroduced as hand-rolled Go, re-opening the injection bug class the rewrite is meant to close

§3 / `internal/chain`:
> "Layer-1: .docker-env-chain parse + token substitution
> (${ENV}/${COMPOSE_ENV}/${HOST}/${HOSTNAME}) — pure Go strings"

§1 frames the legacy `sed`-substitution injection as a key driver:
> "the `sed`-based substitution produced an injection-class bug (a `|`/`&`
> hostname crashed the engine — found and fixed)."

"pure Go strings" removes the *shell/sed* injection vector, which is good. But
Layer-1 is still a templating layer that interpolates a **host-derived** value
(`${HOST}`/`${HOSTNAME}` from `hostname` cmd, §4 step 1) into file paths. The
legacy code sanitizes HOST (`lib/compose-env.sh:75` substitution is preceded by
host sanitization). The spec says "sanitized" for HOST in §4 step 1 but says
nothing about what `chain` does with a token value that, after substitution,
produces a path containing `..`, an absolute path, or a glob/`,` (the
`COMPOSE_ENV_FILES` separator). A `,` in a substituted path silently splits one
entry into two. Per the carried safety rules, this is exactly the
"unsanitized values" class to watch.

**Fix:** Add to §4/§9 an explicit `chain` sanitization + validation contract:
HOST/ENV charset whitelist; reject or escape `,` in any resolved path; define
behavior for path traversal. Add a `chain` unit test that is **RED on a naive
implementation** (e.g. a hostname `a,b` or `a|b`) so the guard proves the fix.

### W2 — `COMPOSE_DEPTH` fate is left undecided, but the spec elsewhere relies on it being gone

§5:
> "with `COMPOSE_DEPTH` likely obsolete once include-graph resolution replaces
> glob discovery"

§12:
> "**`COMPOSE_DEPTH` fate** — likely obsolete (include-graph replaces glob);
> decide whether to drop it or keep as a no-op/back-compat alias."

This is genuinely undecided ("likely", "decide whether"), yet §8 commits to
passing the 61-assertion `smoke-monorepo` suite unchanged, and the legacy engine
reads `COMPOSE_DEPTH` (`lib/compose-env.sh:57` `_DEPTH=${COMPOSE_DEPTH:-3}`,
used at `:92` `find … -maxdepth "$_DEPTH"`). If any smoke assertion sets or
asserts `COMPOSE_DEPTH` behavior, "drop it" fails acceptance while "no-op alias"
passes. The decision is a prerequisite for the acceptance-port plan (task #2),
not a post-v1 cleanup.

**Fix:** Resolve in the spec now: keep `COMPOSE_DEPTH` as an accepted-but-ignored
back-compat alias for v1 (lowest acceptance risk), and have task #2 grep the
smoke suites for `COMPOSE_DEPTH` to confirm. Move it out of §12 "open items"
once decided.

### W3 — "Dedup against Layer 1 (Layer 1 wins)" contradicts the carried secrets-last-wins rule unless scoped

§4 step 3:
> "Dedup against Layer 1 (Layer 1 wins)."

CLAUDE.md carried rule:
> "secrets load **last** in the chain (last-wins)."

These are about different things (file-list dedup vs. variable precedence within
`COMPOSE_ENV_FILES`), but the spec never says so, and "Layer 1 wins" reads as a
precedence statement. Since `.secrets.env` is a Layer-1 chain entry that must win
over earlier entries, and `COMPOSE_ENV_FILES` is *last-wins by file order*, the
final emitted order must keep `.secrets.env` last among Layer-1 entries while
Layer-2 files come after Layer-1. The legacy emit order is
`<Layer 1, chain order>,<Layer 2, discovery order>` (`docs/concepts.md:93`). If
a service env_file (Layer 2) re-defines a secret var, Layer-2 would win at
runtime — that may be wrong.

**Fix:** Clarify in §4 that "Layer 1 wins" means *de-duplication only* (a path
in both layers is emitted once, in its Layer-1 position), and state the variable
precedence explicitly (chain order, secrets last). Add an acceptance assertion
that a secret var is not clobbered by a Layer-2 env_file.

### W4 — Vendored mode "MAY require the Go toolchain" understates a real UX regression vs. the stated distribution goal

§2:
> "Vendored mode MAY require the Go toolchain (the maintainer accepted this) …
> both distribution modes are first-class."

§6:
> "a small POSIX `./cenvkit` shim that execs `go run ./cmd/cenvkit "$@"` …
> Requires a Go toolchain in vendored mode (accepted). No committed binaries, no
> network."

`go run` on every `./cenvkit compose up` recompiles (or hits the build cache)
and adds latency to a command that currently is instant POSIX `sh`. The legacy
kit's selling point (§12) is "zero-toolchain vendoring." Calling both modes
"first-class" while one needs Go + per-invocation `go run` is optimistic. This
is accepted by the maintainer so it is not Critical, but the spec should not
gloss the per-invocation cost.

**Fix:** In §6/§12 note the per-invocation `go run` build-cache cost and the Go
floor for vendored mode; consider documenting `go build` into a gitignored local
binary as the recommended vendored path (still no committed binary).

### W5 — `cenvkit init` carries the bootstrap's secret-handling rule but the spec only asserts the negative

§5:
> "`cenvkit init` … no sudo/chmod/persisted secrets."

§3 `internal/bootstrap`:
> "`cenvkit init` (port of the no-sudo/no-chmod init.sh)"

Good that sudo/chmod/persisted-secrets are called out. But the legacy concern
(noted in the agent brief) was a **secret-wipe** regression: a no-clobber seed
that overwrites an existing populated `.secrets.env` would destroy secrets. §5
says "seed `.X` from `example.X` no-clobber" — the no-clobber is stated, which is
correct, but there is no acceptance assertion guarding it.

**Fix:** Add an acceptance assertion (task #2) that `cenvkit init` against a repo
with an existing non-empty `.secrets.env` leaves it byte-identical. Guard must be
RED against a clobbering implementation.

---

## Suggestions

### S1 — Pin the compose-go version in the spec, not just "confirm"

§12:
> "**Version floor** — Docker Compose ≥ 2.24 already required; confirm the
> compose-go version that matches the targeted compose features."

§10 says "Pin a compose-go version" but no version is named anywhere. Name the
intended floor (e.g. the compose-go tag shipped with Compose 2.24+) in §10 so
`go.mod` review has a target. (Resolved by task #1.)

### S2 — Spec lacks a stated error-behavior for a missing/malformed `.docker-env-chain` and for compose-go load failure

§9 says "Real Go errors with actionable messages (replacing silent `sh`
failures)" but the legacy behavior is *skip missing files silently*
(`lib/compose-env.sh` keeps "existing files in order", §4 step 2: "keep existing
files in order"). Switching missing-file-skip to a hard error would break parity.
State which failures are fatal vs. skipped so it doesn't diverge during
implementation.

### S3 — `cenvkit validate` semantics vs. the active project should be defined

§5:
> "`cenvkit validate` — `docker compose config -q` for the active project
> (dev/prod)."

"(dev/prod)" is ambiguous — does validate run *both* environments, or the
currently-resolved one? Define it; it affects exit-code semantics and CI use.

### S4 — Acceptance #1 says "≈61 assertions" but §1/§8 say 61 exactly

§1: "61-assertion smoke suite"; §8: "(61 assertions)"; §13: "(≈61 assertions)".
Minor, but pick the exact number so the acceptance gate is unambiguous (a flaky
"≈" invites a passing run with fewer assertions). Confirm via task #2.

---

## Cross-cutting note for the lead

The two Critical items (C1 circular interpolation, C2 unverified compose-go env_file
API) are both **prerequisites for `internal/engine`** and both depend on task #1's
compose-go research landing a concrete `file:line` API path. Recommend gating the
engine task on task #1 and folding C1's resolution model + C2's API citation into
§4/§12 before any engine code is written. W1/W3/W5 should each become a guard
test that is RED on pre-fix code (per the guard-validity rule).
