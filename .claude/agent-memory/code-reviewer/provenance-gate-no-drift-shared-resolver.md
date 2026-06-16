---
name: provenance-gate-no-drift-shared-resolver
description: v2 provenance "gate drift" finding is real=false — cmd gate and Provenance both call the SAME resolveComposeFiles(dir, cr.Vars); Provenance is UNGATED on the cmd side
metadata:
  type: project
---

The v2 rich-provenance plan (`docs/superpowers/plans/2026-06-16-cenvkit-rich-provenance.md`)
was accused (coverage-consistency reviewer, "major") of a HasComposeFile seam-drift:
cmd gate `HasComposeFileEnv(dir, cr.Vars)` vs Provenance-internal
`resolveComposeFiles(in.ProjectDir, in.Env)` "can drift" and silently drop Effects.

**Verdict: real=false.** Two reasons, both verified on disk 2026-06-16:

1. **Same pure function, same args → cannot drift.** cmd gate (plan line 624) is
   `HasComposeFileEnv(dir, cr.Vars)` which IS `len(resolveComposeFiles(dir, cr.Vars))>0`
   (discover.go:85-87). Provenance (plan lines 388-391) computes
   `resolveComposeFiles(in.ProjectDir, in.Env)` with `in.ProjectDir==dir`,
   `in.Env==cr.Vars` (cmd never sets `ConfigFiles`). Identical inputs to a pure
   function. The finding conflates `pf` (the EnvFiles A-attribution list, built from
   cr.Files/er.EnvFiles) with `configs` (the compose config list) — different things.

2. **Provenance is UNGATED on the cmd side.** Plan lines 624-634 gate ONLY
   `engine.Resolve` + Layer-2 `pf` building; `engine.Provenance` (line 635) is OUTSIDE
   the `if`, called unconditionally. Provenance does its OWN gating via the shared
   resolver. So the finding's premise ("Provenance only invoked inside the if") is
   factually wrong.

3. **The proposed test already exists.** T2 Step 1 `TestProvenance_BLite_And_C`
   (plan lines 288-330) writes compose.yaml to t.TempDir() with NO COMPOSE_FILE in env
   and asserts Effects non-empty AND Services non-empty — exactly the "make Provenance
   single source of truth + add docker-free test" fix requested.

**Why this matters:** This is the INVERSE of [[has-compose-file-gate-seam]]. That was a
real v1-PLAN defect — two INDEPENDENT reimplementations of COMPOSE_FILE logic. It has
since been FIXED: current discover.go shares one `resolveComposeFiles` between gate and
Resolve (the exact remedy the memory prescribed). The v2 plan inherits that fixed seam.
Do not flag a shared-single-resolver design as "seam drift" — drift requires TWO
implementations, not two calls to one function.

**How to apply:** When a reviewer cites the HasComposeFile seam-drift class, first check
whether the two sites call the SAME helper with the SAME args. If yes → real=false. Only
independent reimplementations (or differing args, e.g. raw vs interpolated COMPOSE_FILE)
constitute drift.
