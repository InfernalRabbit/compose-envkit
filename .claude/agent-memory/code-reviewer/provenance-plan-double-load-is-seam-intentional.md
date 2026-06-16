---
name: provenance-plan-double-load-is-seam-intentional
description: v2 provenance T3 double engine.New() + double LoadProject (Resolve then Provenance) is correct & seam-intentional, not a defect
metadata:
  type: project
---

The v2 rich-provenance plan's T3 RunE (plan lines 624-639) calls
`engine.New().Resolve(...)` then `engine.New().Provenance(...)`. Both `Resolve`
and `Provenance` independently run `resolveComposeFiles` + `cli.LoadProject`, so
the compose project is loaded twice in the env-debug path.

Verdict: NOT a defect. `composeEngine` is `struct{}` (engine.go:41), stateless;
double `New()` is free (zero-size alloc). The double `LoadProject` is the
intended cost of the containment seam: `Resolve` returns a compose-go-FREE
`Result{EnvFiles, ProjectView}` (engine.go:22-34) so cmd/debug never import
compose-go. A loaded `*types.Project` can't cross the public seam, so each entry
point owns its own load. Output is identical & deterministic (both use
`WithoutEnvironmentResolution`, no side effects).

**Why:** reviewers keep filing "wasteful / construct once / fold the load"
findings against this plan; they are efficiency/style, graded minor by their own
authors, and the "fold Layer-2 into Provenance" variant actually fights the
chain-owns-Layer-1-ordering design (A-attribution at provenance.go:396 iterates
the cmd-assembled Layer1+Layer2 merge).

**How to apply:** for this plan, treat double-load / construct-once findings as
real=false unless they show a correctness or compile break. The double load is
load-bearing architecture, not an oversight. Distinct from genuine plan defects:
[[provenance-plan-spec-drift]].
