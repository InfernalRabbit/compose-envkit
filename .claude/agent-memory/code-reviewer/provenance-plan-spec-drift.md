---
name: provenance-plan-spec-drift
description: cenvkit v2 provenance plan silently re-architects vs its own spec (ProvenanceFacts/Build/chain-A dropped); self-review "spec coverage" table lies
metadata:
  type: project
---

The 2026-06-16 cenvkit rich-provenance PLAN diverges from its own SPEC on the
core data flow, without saying so:

- SPEC §4/§5/§6: `engine.Provenance(ctx, Input) -> engine.ProvenanceFacts`
  (a compose-go-FREE intermediate: `ProvenanceFacts{Services []ServiceEnvFacts;
  Effects map[string][]EffectFact}`), chain attribution (A) done in
  `internal/chain` via an exposed `engine.ParseDotEnv` helper, then a pure
  `provenance.Build(chainAttr, facts) -> Report` assembles the model.
- PLAN: `engine.Provenance(ctx, ProvInput) -> provenance.Report` directly. No
  `ProvenanceFacts`, no `Build`, A done INSIDE engine over `in.EnvFiles`. `engine`
  imports `provenance` (opposite coupling direction from the spec sketch).

The plan's approach is arguably cleaner, but the plan's own Self-review claims
"§5 model -> T1" and "§4 architecture ... -> file structure" as DONE — false:
T1 has no `ProvenanceFacts`, and the chain-A path the spec mandates is gone.

**Why:** recurring class — author re-architects during planning, then the
coverage self-table maps spec sections to tasks by section NUMBER not by actual
content. A green self-review hides the drift.

**How to apply:** when a plan claims spec coverage, diff the spec's TYPE NAMES
and FUNCTION SIGNATURES against the plan's, not just the section list. Flag any
spec type (ProvenanceFacts, Build, ParseDotEnv) that has no home in the plan as
a coverage gap OR demand the spec be updated to match. Relates to
[[plan-consistency-defect-classes]].
