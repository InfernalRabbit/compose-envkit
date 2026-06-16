---
name: provenance-a-vs-blite-os-overlay-drift
description: v2 provenance A-attribution reports raw file Value but B-lite/C resolve with OS-overlaid in.Env → same-var inconsistency; spec wants effective Value + environment layer
metadata:
  type: project
---

cenvkit v2 rich-provenance plan (Task 2 Step 3, `internal/engine/provenance.go` A loop ~lines 396-411): the A-attribution `vt.Value` is set from the raw `parseDotEnv(file)` winner, but B-lite's `mapping` (and the chain Vars seed) come from `in.Env` = `cr.Vars`, which chain.go (chain.go:189-196) builds as file-vars-then-OS-overlay (shell wins, last-wins). So a var set in BOTH a chain file AND the shell gets `--trace`/`--value` reporting the stale FILE value while the B-lite effect's `Resolved` uses the SHELL value — internally inconsistent single-var output, and the reported winning value disagrees with what docker compose actually interpolates.

**Why:** real=true MAJOR (verified 2026-06-16). Spec §5 (line 89) defines `Source.Layer` enum INCLUDING `environment`, and (line 96) `Value` = "effective (winning) value" — the plan's file-only A loop never models the OS layer, diverging from the spec. v1 debug.go Value/Trace was ALSO file-only but had no B-lite, so v1 was internally consistent; v2 introduces the drift by mixing file-based A.Value with OS-overlaid B-lite.Resolved.

**How to apply:** preferred fix = append a synthetic `Source{Layer:"environment"}` winning over all files (keeps Winner/Overridden/Value mutually consistent and uses the `environment` layer the spec already declares); the "just overwrite Value from merged env" option creates a Winner≠Value mismatch. Watch this class on any plan that has a file-attribution pass + a separate interpolation pass fed from the OS-overlaid chain Vars. Related: [[provenance-mapping-missing-layer2]] (the inverse — Layer-2 vars MISSING from the interp seed).
