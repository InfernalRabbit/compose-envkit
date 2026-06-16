---
name: s4-acceptance-count-drift
description: cenvkit acceptance gate hardcodes "61 exact" but G1-G5 inversions change assertion count; the count delta is left as a TODO in the plan
metadata:
  type: project
---

The cenvkit spec §13.1 acceptance criterion #1 says the ported `smoke-monorepo`
suite must pass "**61** assertions — exact, S4". But the deliberate compose-go
inversions (G1-G5) do not all preserve count: scenario 11 (COMPOSE_DEPTH
boundary, 11.1+11.2) loses at least the 11.2 depth-behavior assertion because
`a/b/c/docker-compose.yml` is a standalone file never in the root `include:`, so
the include-graph never enumerates it regardless of COMPOSE_DEPTH (untestable as
written). Scenarios 9/10 are over-discovery cases (stray/renamed compose NOT in
include:) that also invert.

**Why:** the spec-audit S4 finding warned that "≈61" vs "61 exact" makes the
acceptance gate ambiguous. The implementation plan's Task 7 Step 6 then DEFERS
the count ("if so, document the new total ... with the lead's sign-off") instead
of resolving it — re-introducing the exact ambiguity S4 flagged.

**How to apply:** when reviewing the cenvkit acceptance task, require the plan to
state the FINAL exact post-inversion assertion number and update spec §13.1 to
match, rather than asserting a stale 61 with a TODO. Note the subtlety: scenario
10.2 ("renamed docker-compose.yml IS discovered") is ALSO over-discovery under
compose-go (the renamed file is not in the root include:), so the simple
"inversion = same count" arithmetic is itself wrong — the delta needs a concrete
per-scenario recount, not a hand-wave. Related: [[glob-vs-include-acceptance-class]],
[[plan-consistency-defect-classes]].

**RECURS in v2 provenance plan (2026-06-16):** T4 Step 2 (plan line 706) again
DEFERS the recount verbatim ("record the new exact acceptance total (was 60) and
pin it. Lead signs off"). The acceptance file now pins the count in the HEADER in
two literal spots — `test/cenvkit-acceptance_test.go:1` ("60 assertions after S4
recount") and `:17` ("exactly 60 smoke-monorepo assertions"), plus `:18` ("NOT
counted in 60") and per-scenario `(N assertions)` annotations. T4 never names
these header sites as edit targets → real consistency drift, finding real=true.
Arithmetic nuance: it is NOT "60 + new". T4 Step 1 says `--value SMOKE_VAL`
*replaces* the v1 raw assertion (existing scenario 5.6, file lines 495-507), so
the new total is add-AND-replace — a per-scenario audit, not "60 + count(new)".
