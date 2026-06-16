---
name: plan-consistency-defect-classes
description: Recurring internal-consistency defect classes in cenvkit implementation plans (dep-graph label drift, debug --value layer scope, chain↔engine COMPOSE_FILE seam)
metadata:
  type: project
---

Recurring defect classes found reviewing the cenvkit v1 implementation plan
(`docs/superpowers/plans/2026-06-15-cenvkit-v1-implementation.md`). Check these
first on any future cenvkit plan revision.

**Why:** these are the defect classes that survived the plan author's own
self-review section — they slip past a green-looking artifact because the
author's "type consistency" check only verified struct field names, not the
prose dependency graph or the layer-scope of CLI subcommands.

**How to apply:** when reviewing a cenvkit plan, mechanically cross-check:

1. **Dep-graph label drift vs task headings.** The ASCII graph + `blockedBy`
   bullets label nodes by package (T4 debug, etc.). Diff every node label
   against the actual `## Task N:` heading text. In the 2026-06-15 plan the graph
   said "T4 debug" but Task 4 was `internal/envfiles`; `internal/debug` was
   folded into Task 6. The numeric blockedBy stayed correct by luck but the
   labels mislead the lead seeding the task-list.

2. **`env-debug --value` / `--diff` layer scope.** Legacy `lib/env-debug.sh`
   sources `--value` from ONLY the Layer-1 project chain (`.docker-env-chain`),
   explicitly NOT Layer-2 container env_file paths (those carry bare `${...}`
   compose-interpolation refs unsafe to shell-source — see lib/env-debug.sh:111-117).
   Any plan that passes the MERGED list into the value/trace helper diverges and
   can flip a smoke assertion if a Layer-2 file sets a queried var. Cross-check
   [[carried-bug-classes-cenvkit]] secret-wipe class too: a secret var must not
   be re-sourced from Layer-2.
   RECURRED in the v2 rich-provenance plan
   (`docs/superpowers/plans/2026-06-16-cenvkit-rich-provenance.md`): T2 builds
   `Report.Vars` (the A phase) from the FULL ordered `EnvFiles` (Layer-1 `pf` +
   Layer-2 `er.EnvFiles`), and T3 `RenderHuman`'s `Value` case prints
   `r.Vars[o.Value].Value`. So `--value` now reflects Layer-2 last-wins —
   shadows Layer-1 and can surface a secret, regressing smoke.sh:218 /
   `TestEnvDebug_Value` (the existing test passes only by luck: its fixture has
   no compose file → no Layer-2). Fix: `--value` must read a Layer-1-only trace
   (drop Layer-2 sources before computing the winner for `--value`), OR the spec
   must explicitly redefine `--value` semantics and the acceptance test must be
   updated with lead sign-off. See [[env-debug-layer-scope-per-mode]].

3. **chain↔engine COMPOSE_FILE seam.** `chain.parseDotEnv` does NOT interpolate
   `${...}`, so `cr.Vars["COMPOSE_FILE"]` reaches the engine/gate with tokens
   intact. A `HasComposeFile` gate that `os.Stat`s the raw (un-substituted) value
   only works because the FIRST path entry happens to be token-free. Flag
   single-pass gates that stat un-interpolated COMPOSE_FILE entries.

4. **Self-review "ACTION for TN" that never lands as a checkbox step.** The
   author's self-review section can identify a gap ("add to T7"), tick it ✓ as
   resolved, yet never fold an executable numbered step into the task body. An
   executor working the checklist skips it. In the 2026-06-15 plan, the **C1
   guard** (an `env_file:` path referencing a Layer-2-only var must be
   unsupported / not silently mis-resolved — spec §4a, §13 item 3) was listed in
   §13's RED-guard set ALONGSIDE W1/W3/W5/D1, but every other guard got its own
   executable RED step (W1=T2.1, W3=T4.1, W5=T5.1, D1-assembly=T3.2) while C1
   lived only in the self-review prose (plan lines 1728, 1739) — Task 7 body
   (1566-1662) Steps 1-8 never mention it. Mechanical check: grep the plan for
   each spec §13 guard ID; every one must resolve to a `- [ ] **Step`, not just a
   self-review bullet. An orphaned guard leaves a spec contract unpinned by
   acceptance. See [[carried-bug-classes-cenvkit]] for why guard-RED-on-pre-fix
   matters. NB: item 4 maps "W3=T4.1" but that mapping is itself wrong — see
   item 5 (T4.1 tests ordering, not the value precedence W3 actually requires).

5. **W3 secrets-last: ordering test ≠ value-precedence guard.** Spec §13.3
   requires a guard, RED on naive impl, that "a Layer-2 `env_file:` does NOT
   clobber a secret var (`.secrets.env` last)" — a VALUE-level claim resolved at
   `docker compose config` load time. In the 2026-06-15 plan the only W3 test
   (T4 Step 1 `TestAssemble_OrderDedupSecretsLast`, plan lines 937-951) operates
   on `[]string` paths and asserts FILE ORDERING/dedup only; self-review line
   1730 maps "§4c precedence/dedup (W3) → T4 ✓", over-claiming coverage.
   spec-audit.md:137-158 explicitly separates the two concerns ("If a service
   env_file (Layer 2) re-defines a secret var, Layer-2 would win at runtime —
   that may be wrong"). Fix: add a docker-gated T7 step — fixture sets a var
   (e.g. API_TOKEN) in BOTH `.secrets.env` AND a Layer-2 service `env_file`, run
   `cenvkit compose config`, assert the `.secrets.env` value wins. Subtlety:
   §4c's emitted order is Layer-1-then-Layer-2, so a colliding Layer-2 file lands
   AFTER `.secrets.env` in last-wins file order — the guard may surface that the
   design itself lets Layer-2 clobber secrets (the real bug W3 was meant to
   catch). Must be docker-gated: value resolution happens at compose load, not in
   the pure-Go `Assemble`.

6. **Recommendation-in-a-comment with no executable step (hygiene variant of
   item 4).** Distinct from item 4 (self-review prose): here the plan recommends
   an action inside a generated *artifact's comment* and never makes it a step. In
   the 2026-06-15 plan, the Task 8 vendored shim comment (lines 1678-1679) says
   `go build -o .cenvkit.bin … (add .cenvkit.bin to .gitignore)`, and the spec
   twice mandates "no committed binaries" (spec lines 40, 210; trade-off note
   302-304 recommends the gitignored local binary). But no Task 8 step edits
   `.gitignore` — current `.gitignore` ignores only env files / editor noise /
   `/tmp-smoke/`. Severity minor: doesn't break build or the 61-assertion suite
   (`.cenvkit.bin` is an opt-in fast path; default shim is `go run`), but a built
   binary could be committed, violating a spec invariant. Extra subtlety: a naive
   `go build ./cmd/cenvkit` emits `./cenvkit`, which COLLIDES with the tracked
   POSIX shim file also named `cenvkit` (plan line 1671) — so the ignore must NOT
   blanket-ignore `cenvkit`; ignore `.cenvkit.bin` and goreleaser `dist/` only,
   and the plan should warn about the name collision. Ownership: `.gitignore` is
   repo-config → architect/lead per the module-boundary table (go-engineer only
   with lead approval). Mechanical check: for every artifact-comment that says
   "add X to .gitignore" / "gitignored", confirm a real step edits `.gitignore`.

7. **Test-file import block omits an import used by a LATER appended step.** The
   plan writes a `_test.go` import block at the TDD-RED Step 1, then appends more
   test functions in a later step (Step 5) using a package the original block
   never imported → `undefined: X` compile failure at the full-suite run. In the
   2026-06-15 plan, `internal/chain/chain_test.go` Step 1 import block (lines
   199-203) is only `os`, `path/filepath`, `testing`; Step 5's appended
   `TestChainOrderingAndEnvSwitch` (line 519) calls `strings.Join(got, ",")` but
   no step ever adds `"strings"` to the test file (the six `"strings"` imports in
   the plan — lines 287, 755, 974, 1120, 1249, 1585 — are all in PRODUCTION
   blocks: chain.go/engine.go/assemble.go/etc., none in a `_test.go` block).
   Fix: add `"strings"` to the Step 1 chain_test.go import block. Owner:
   qa-engineer. **Subtlety on impact:** the go-correctness reviewer claimed this
   breaks the Step 2 RED check ("fails for the wrong reason, build error not
   `undefined: Resolve`") — that part is WRONG: the `strings.Join` code is
   appended at Step 5, AFTER impl (Step 3) and after W1 tests pass (Step 4), so at
   Step 2 the file only holds the two W1 tests (neither uses `strings`) and Step 2
   still fails correctly with `undefined: Resolve`. The genuine break is at Step 6
   (`go test ./internal/chain/ -v`) which won't compile. Mechanical check: for any
   plan that "appends" to a `_test.go` in a later step, collect every
   `pkg.Symbol` in the appended block and confirm each `pkg` is in the file's
   ORIGINAL import block — `gofmt -l` is a check not a fixer, so it won't add it.

See also [[spec-circular-interpolation-class]] (the single-pass design tension)
and [[carried-bug-classes-cenvkit]] (secret-wipe class).
