---
name: v3-layer2-debug-only-gap-detector
description: v3 (2026-06-17) reverses Layer-2 run-path injection; run = L1-only COMPOSE_ENV_FILES, env-debug becomes a gap-detector. Verified levers + mechanism.
metadata:
  type: project
---

v3 spec `docs/superpowers/specs/2026-06-17-cenvkit-layer2-debug-only-design.md`
(owner-confirmed 2026-06-17) makes a service `env_file:` RUNTIME-ONLY. Reverses
the v1/v2 run-path env_file→interpolation injection.

**Why:** folding every active service env_file into one flat COMPOSE_ENV_FILES
collapses a shared key (`${PORT}`) into a single GLOBAL interpolation value
(compose interpolates the whole YAML against ONE env map before per-service
split). Per-service scope is impossible without forking compose-go (forbidden).
Owner judged global injection illogical as a default.

**How to apply (the two levers, both verified against the real code):**
- **Run path L1-only:** the minimal lever is `cmd/cenvkit/main.go` `assemble()` —
  stop passing `er.EnvFiles` into `envfiles.Assemble`. Keep `engine.Resolve`
  emitting `Result.EnvFiles` UNCHANGED so the seam test (`test/seam_test.go`,
  qa-owned, asserts L2 in the merged list via the engine directly) is untouched
  by the cmd change; the run path simply no longer consumes L2. `internal/chain`
  unaffected; `internal/envfiles.Assemble` unaffected (still merges two lists —
  cmd just hands it `nil` for L2).
- **Gap-detector interpolation mapping:** in `internal/engine/provenance.go` the
  `chainEnv` build currently overlays L2 env_files (the `for i, f := range
  in.EnvFiles` loop folds ALL layers into `chainEnv`). v3: build the B-lite
  `mapping` from **layer1 files + in.Env only** (skip `f.Layer=="layer2"` when
  populating the interpolation env). Then `SubstituteWithOptions(leaf, mapping,
  WithoutLogging)` returns the `:-default` for an L2-only `${V}` = the REAL run
  value. A (chain attribution) and C (--effective per-service native env) stay as
  they are; only the interpolation `mapping` drops L2.

**Gap condition (spec §3.1):** for var V referenced in a service YAML field,
`Gap = referenced && !InChain && len(RuntimeDefs)>0` where InChain = V resolvable
from the L1 interpolation env, RuntimeDefs = V defined in some active service
env_file (gathered via `dotenv.ReadFile(svc.EnvFiles)`, already done for C).

**Mechanism feasibility VERIFIED (go doc + module source, v2.11.0, 2026-06-17;
context7/serena not used — go doc primary-source probe):**
- `template.Mapping = func(string)(string,bool)` — bool is "is present".
- `template/template.go:323` `withDefaultWhenAbsence` for `:-`: when
  `mapping(name)` is `!ok`, returns the default. So an L1-only mapping yields the
  fallback for an env_file-only var. This is the whole §3.2 step-3 claim, proven.
- `dotenv.ReadFile(path, LookupFn)` and `template.ExtractVariables(dict, pattern)`
  unchanged from v2 — no new compose-go surface.

Canonical fixture gap: `examples/monorepo/web` — `WEB_PORT` referenced in
`services.web.ports[0]` (`"${WEB_PORT:-0}:80"`) + `services.web.environment.WEB_PORT`,
defined ONLY in `web/.web.env` (WEB_PORT=18080). Same for API_PORT/api.

See [[compose-go-blite-provenance]], [[provenance-chain-vs-files-view]],
[[d1-lenient-enumeration-lever]].
