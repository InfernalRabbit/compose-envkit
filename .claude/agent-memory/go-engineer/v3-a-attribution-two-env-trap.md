---
name: v3-a-attribution-two-env-trap
description: v3 gap-detector — EVERYTHING that interpolates (A-attribution, A overlay, B-lite mapping, C-load WithEnv, details.Environment) must read interpEnv not chainEnv; chainEnv's only role is the dotenv lookup
metadata:
  type: feedback
---

In `internal/engine/provenance.go` the v3 plan said "L1-only A-attribution falls
out of T4+T7, no extra code" — that is only true at RUNTIME (T7 makes
`in.EnvFiles` Layer-1-only). It is NOT true for the engine called directly with
mixed layers (the seam/unit path qa exercises). Two extra guards were required to
keep the engine correct regardless of caller:

1. A-attribution loop: `continue` on `f.Layer != "layer1"` so a Layer-2 service
   `env_file:` never sets a `Winner`/`Value` (those attribute over the
   INTERPOLATION env only — spec §3.3).
2. A overlay loop: iterate `interpEnv` (L1 + shell), NOT `chainEnv` (all layers).
   `chainEnv` holds Layer-2 file values, so iterating it would set
   `Winner=(environment), Value=<layer2 value>` for a var that has no L1 winner
   (`vt.Winner.File==""`) — a false chain winner that breaks `InChain`/`Gap`.
3. C resolved-load `cli.WithEnv` (the `mergedEnv`) AND the raw-load
   `details.Environment` must ALSO read `interpEnv`, NOT `chainEnv`. The plan §3
   line "keep chainEnv for C, intentional and correct" was WRONG — the lead
   corrected it 2026-06-17. An inline `environment:` `${X}` interpolates against
   COMPOSE_ENV_FILES (=L1) + host at the real run, so `--effective` must show the
   fallback for an env_file-only `${X}` (verified in-process:
   `EFFECTIVE_PORT: "${WEB_PORT:-0}"` with WEB_PORT only in .web.env renders
   `EFFECTIVE_PORT=0`, while the literal `WEB_PORT=18080` env_file entry is still
   shown verbatim). With chainEnv it would render `18080` — a value the container
   never gets (spec §3 forbids reporting a resolution the run won't produce).

**Why:** the two env contexts are the whole v3 model — `interpEnv` (L1+shell) is
the real run interpolation env and must drive EVERYTHING that interpolates:
B-lite `mapping`, A-attribution, A overlay, `InChain`, the C-load `WithEnv`, and
`details.Environment`. `chainEnv` (all layers) has exactly ONE remaining role: the
`dotenv.ReadFile` lookup closure (in-file `${VAR}` refs inside an env_file).
Mixing them reintroduces the #3435 "env_file resolves at interpolation" bug.

**How to apply:** any future provenance edit that touches winner/value/InChain OR
interpolation (`WithEnv` / `details.Environment` / `SubstituteWithOptions`) must
read `interpEnv`. The ONLY legitimate `chainEnv` reader is the dotenv lookup.
Verify `--effective` on an inline-references-env_file-only-var case shows the
fallback, not the env_file value. See [[compose-go-blite-provenance]] and
[[v3-layer2-debug-only-gap-detector]]. The `:-` fallback that makes a Layer-2-only
`${WEB_PORT:-0}` render `0:80` is verified at compose-go `template/template.go:323`
(withDefaultWhenAbsence).
