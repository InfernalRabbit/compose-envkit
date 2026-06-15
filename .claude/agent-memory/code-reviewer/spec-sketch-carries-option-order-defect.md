---
name: spec-sketch-carries-option-order-defect
description: cenvkit SPEC §4 engine sketch (not just the plan) has the broken compose-go option order (WithConfigFileEnv before WithEnv) + the false "WithConfigFileEnv honors/interpolates COMPOSE_FILE" claim — fix the spec too, it's the authoritative source
metadata:
  type: project
---

The cenvkit option-order / COMPOSE_FILE-interpolation defect class lives in TWO
places, and reviewers tend to only flag the plan:

- **Plan** Task 3 Step 4 (lines 699-708): `cli.WithConfigFileEnv` before
  `cli.WithEnv(in.Env)` — but the plan ALSO has a Step 9 (lines 878-891)
  de-risk/decision-rule that empirically checks and falls back to manual
  COMPOSE_FILE read+interpolate+split. So the plan is partially self-correcting.
- **Spec** §4 step 3 (lines 115-122): SAME broken order (WithConfigFileEnv line
  117 before WithEnv line 119) AND the comment "honor COMPOSE_FILE + resolve
  include: (the no-glob win)" — which overclaims: WithConfigFileEnv reads
  `o.Environment[COMPOSE_FILE]` (empty unless WithEnv ran first) and does NOT
  interpolate `${COMPOSE_ENV}` inside COMPOSE_FILE. The spec has NO Step-9
  equivalent and is labeled "authoritative" (plan line 15), with §10 the bump
  checklist. So a future re-impl / compose-go bump that follows the spec sketch
  verbatim reintroduces the bug (scenario 15/8 silent chain-only).

Verdict 2026-06-15 (composego-fidelity reviewer finding, severity minor):
**REAL.** Both factual claims hold (probe-verified, see
[[compose-go-option-order-and-compose-file]]). Fix the SPEC: (a) reorder so
WithEnv precedes WithConfigFileEnv AND WithDefaultConfigPath (both read
o.Environment); (b) replace the "honor COMPOSE_FILE" comment with a note that
COMPOSE_FILE selection+`${...}` interpolation is done in cenvkit code (manual
read-from-seed-env, interpolate, split on COMPOSE_PATH_SEPARATOR-else-`:`), NOT
by WithConfigFileEnv. Section-ref nuance: finding said "§12 (bump checklist)" but
§10 is the actual bump-and-re-run-acceptance policy; §12 is risk items. Best
homes: §4 sketch itself + a one-line caveat in §10.

**How to apply:** when a reviewer flags the plan's compose-go wiring, also check
whether the SPEC sketch carries the same shape — the spec is the durable source
of truth and outlives a given plan. Relates to
[[plan-consistency-defect-classes]] (chain↔engine COMPOSE_FILE seam) and
[[has-compose-file-gate-seam]].
