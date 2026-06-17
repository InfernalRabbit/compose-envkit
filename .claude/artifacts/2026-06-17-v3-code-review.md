# cenvkit v3 (Layer-2 debug-only) — code review

Reviewer: code-reviewer · Date: 2026-06-17 · Scope: uncommitted diff over
`cmd/ internal/ test/` (frozen tree). Spec: `docs/superpowers/specs/2026-06-17-cenvkit-layer2-debug-only-design.md`
(§8 D1–D5). Plan: `.claude/artifacts/2026-06-17-layer2-plan.md`.

Verification run locally (read-only): `go build ./...` OK · `go vet ./...` OK ·
`SMOKE_SKIP_DOCKER=1 go test ./...` all packages GREEN · seam invariant holds
(only `internal/engine` imports compose-go; `internal/provenance` pure Go) ·
`gofmt -l` flags ONE file (see SF-1).

## Verdict: CHANGES REQUIRED

Two SHOULD-FIX items (one is a hard DoD-gate failure: gofmt). No BLOCKERs — the
core v3 contract (run path L1-only, gap-detector, D5 --effective fidelity) is
correctly implemented and genuinely guarded. The blocking items are mechanical
+ a count-reconciliation that spec §8 D3 explicitly reserves for lead sign-off.

---

## BLOCKER

None.

The load-bearing correctness invariants all hold:

- **Run path L1-only** — `cmd/cenvkit/main.go:58` `envfiles.Assemble(cr.Files, nil)`;
  `engine.Resolve` dropped from `assemble()` (D1). `chain.Resolve(...)` still runs
  first (`main.go:37`-region), so W1 sanitization (`internal/chain/chain.go:36`
  `sanitizeToken`, charset `[A-Za-z0-9._-]`) and the comma-reject in
  `internal/envfiles/assemble.go:17` are intact — no sanitization bypass.
- **D5 --effective fidelity** — `internal/engine/provenance.go:197-200,260` feed
  `interpEnv` (L1+shell) to both `cli.WithEnv` and `details.Environment`, so an
  inline `environment:` `${X}` that is env_file-only resolves to its fallback.
  Empirically proven GREEN docker-free by `TestProvenance_Effective_InlineEnvInterpolation`
  (prov-6a: `FOO=="fallback"`).
- **Gap logic** — `provenance.go:294-315`: `Gap = referenced && !InChain &&
  len(RuntimeDefs)>0`. `InChain` set for ALL vars BEFORE the chain-only early
  return (`provenance.go:178-181`), then re-set idempotently in the gap loop —
  the chain-only-skip bug is correctly fixed; no other early-return path skips it.
- **Secrets-last** — purely a Layer-1 property now; `chain.Resolve` ordering +
  `Assemble` unchanged. No secrets written to disk / logged in the v3 diff
  (grep clean for chmod/sudo/WriteFile/secret-log in prod files).
- **Determinism** — `Effects`, `RuntimeDefs`, file lists all sorted
  (`provenance.go:295-308`).

## SHOULD-FIX

### SF-1 — gofmt violation (DoD Band A gate failure) · qa-engineer
`test/cenvkit-acceptance_test.go:660-663` (the `TestScenario3` doc comment).
`gofmt -l .` reports this file dirty; `gofmt -d` wants the multi-line comment
continuation reflowed to the tab-indented form:

```
 // 3.3 ports resolve to the 0 fallback — docker compose config expands ${WEB_PORT:-0}:80
-//     to long-form published:"0" (the literal template string never appears in resolved output)
+//
+//	to long-form published:"0" (the literal template string never appears in resolved output)
```

CLAUDE.md / TEAM.md Band A DoD requires "gofmt clean" before integration. This
is a hard gate. Fix: `gofmt -w test/cenvkit-acceptance_test.go`.

### SF-2 — acceptance count: stale bookkeeping + diverges from the D3-locked 72 · qa-engineer (+ lead sign-off)
The header now claims **75** assertions, but the spec §8 D3 and plan §5 both
**locked N=72** (the plan arithmetic `68 −1 +5 = 72` did NOT include the +3
prov-6 invariants). The impl legitimately added prov-6a/b/c, making the real
number 75 — but D3 says: "qa reconciles … locks the final number … Lead signs
off on the final count at integration." So 75 ≠ the planned 72 needs explicit
lead ratification, not a silent bump. Independently, three count comments are
**stale and contradict the 75**:

- `test/cenvkit-acceptance_test.go:586` — "NOT counted in **72**" (plan's number).
- `test/cenvkit-acceptance_test.go:631` — "NOT counted in **68**" (v2 number).
- `test/cenvkit-acceptance_test.go:818` — "8 net-new; count **60→68**" (v2 section).

Fix: reconcile all four to the single locked number (586/631 → "NOT counted in
75"; 818 → drop or reframe the v2 delta), and have the lead ratify 75 at
integration per D3. NOTE: "75" is a manual arithmetic claim — assertion count is
NOT `go test` func count (36 RUN / 30 PASS funcs here), so the number can only be
reconciled by counting PASS sites, which is qa's D3 chore.

## NIT

### N-1 — `TestSeam_RunPath_L1Only` is a weak (near-tautological) guard for the run path · qa-engineer
`test/seam_test.go:1471` calls `envfiles.Assemble(cr.Files, nil)` directly with a
hardcoded `nil` Layer-2, then asserts no service env_file appears. It is GREEN by
construction and would NOT catch a regression that re-added `engine.Resolve` +
`er.EnvFiles` inside `cmd.assemble()` — it doesn't exercise the cmd path. The
sound run-path guard is `TestV3_RunPath_EnvFiles_L1Only` (`:1206`), which drives
the real binary's `env-files` (TestMain builds `../cmd/cenvkit`). Coverage is not
actually missing; the seam test is just redundant/weak. Optionally retarget the
D4-contract-2 assertion at the binary, or document that `TestV3_RunPath_*` is the
real gate and the seam half is a white-box sanity check.

### N-2 — docker-gated 3.3 is normalization-format-coupled · qa-engineer
`test/cenvkit-acceptance_test.go:683` asserts `published: "0"`. Whether
`docker compose config` emits the long form `published: "0"` vs the short form
`0:80` for a fallen-back `${WEB_PORT:-0}:80` depends on the compose-go/docker
version's config normalizer. The negative checks 3.1/3.2 (absence of
`18080`/`19090`) are version-robust and backstop a non-interpolation false-green;
3.3 is the brittle one. Acceptable (passes on the pinned version per the lead's
docker run), but re-verify on any compose-go bump (it's already on the bump
checklist, spec §7).

### N-3 — `--files` runtime group can drop a fully-inline-overridden env_file · go-engineer
`internal/provenance/render.go:587` `serviceEnvFiles` derives runtime-only paths
from C entries with `Source.Layer=="env_file"`. But the C-loop
(`internal/engine/provenance.go:234-247`) overwrites `source[k]` to
`"environment"` when an inline `environment:` key shadows the same env_file key
(inline-wins). If EVERY key of a given env_file is inline-overridden, that
env_file path vanishes from the `--files` runtime group even though it is still a
declared (and read) service env_file. Display-completeness only — no run-path or
gap-detection impact. If `--files` is meant to enumerate declared env_file paths,
derive them from the service's declared `env_file:` list rather than from C
entries.

### N-4 — comment drift: `--files` flag help still says "full COMPOSE_ENV_FILES list" · go-engineer
`cmd/cenvkit/main.go:295` registers `--files` with help
`"full COMPOSE_ENV_FILES list"`, but D2 repurposed it to the two-group
interpolation/runtime view (and `render.go:16` / `HumanOpts.Files` doc was
updated to match). The CLI `--help` text is now stale vs the behavior. One-line
fix to the flag usage string.

---

## Guard-validity check (standing duty)

- `provenance_test.go` WEB_PORT (`Resolved==":80"`, `Gap==true`, `RuntimeDefs`):
  RED on pre-v3 merged-mapping (old code resolved `"8080:80"`, no Gap field). VALID.
- `TestV3_Gap_JSON_Fields` (`Gap==true`): pre-v3 model had no `Gap` field → false.
  RED on pre-v3. VALID.
- `TestV3_NoFalseGap_ChainVar` / `_UnsetEverywhere`: over-eager-gap guards (assert
  `Gap==false`). Correct polarity for catching a future regression; green-from-birth
  on correct impl is acceptable here since pre-v3 had no Gap concept.
- `TestProvenance_Effective_InlineEnvInterpolation` (D5): RED on any impl folding
  service env_file into the interpolation mapping (would show `FOO==envfileval`).
  Runs docker-free. STRONG guard for the D5 fix.
- `TestV3_RunPath_EnvFiles_L1Only`: drives the real binary; RED on a pre-v3
  L2-appending impl. VALID.

## Seam / safety regression check

- Seam: only `internal/engine` imports compose-go (direct-import check, not
  `-deps`); `internal/provenance` clean. HOLDS.
- W1 chain sanitization: intact — `chain.Resolve` still on the run path; comma
  reject still in `Assemble`. No bypass from the `engine.Resolve` drop.
- Secrets: no disk-write / log of secrets in the v3 prod diff; secrets-last is a
  preserved Layer-1 property.
