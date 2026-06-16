---
name: provenance-trace-var-positional-drift
description: v2 plan's own --trace/--value example+acceptance cmds pass VAR positionally but RunE discards args & only --var sets varName → plan-internal breakage, not just spec drift
metadata:
  type: project
---

In the v2 rich-provenance plan (docs/superpowers/plans/2026-06-16-cenvkit-rich-provenance.md),
the env-debug surface is `--trace`/`--value` (bool) + `--var VAR` (string flag),
matching v1 (cmd/cenvkit/main.go:297-299) and the committed acceptance tests
(test/cenvkit-acceptance_test.go:500,514 use `--var SMOKE_VAL`).

But the plan's OWN commands pass VAR positionally:
- Task 3 Step 3 (line 685): `env-debug --trace WEB_PORT`
- Task 4 Step 1 (lines 700,701,703): `--trace WEB_PORT`, `--value SMOKE_VAL`
RunE is `func(cmd, _ []string)` (line 609) → args discarded; `pick(mTrace, varName)`
(line 648) → `pick(true, "")` → empty name → "not set". The plan's smoke + acceptance
steps FAIL against the plan's own flag wiring (clean build, wrong output). Spec §7
(lines 159,164,182) also uses the positional `--trace VAR` notation and never
mentions `--var`.

**Why:** real defect (broken plan acceptance step), but a coverage-consistency
reviewer mislabeled it minor/spec-only. Fix = pick `--var` form (preserves v1
muscle memory + already-passing `--var` tests) and align all THREE: spec §7,
plan Task 3 Step 3, plan Task 4 Step 1.

**How to apply:** when an env-debug finding cites "spec vs plan flag form," also
diff the plan's own example/acceptance command lines against the flag decls — the
plan tends to contradict itself, not just the spec. Recurring env-debug surface
drift; see [[provenance-diff-flag-dead-alias]], [[env-debug-layer-scope-per-mode]],
[[cli-flag-mutual-exclusivity-not-a-defect]].
