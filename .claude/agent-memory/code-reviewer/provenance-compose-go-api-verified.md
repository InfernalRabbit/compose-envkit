---
name: provenance-compose-go-api-verified
description: cenvkit v2 provenance plan Task 2 compose-go v2.11.0 API calls are all signature-correct (probe-reverified); the real defects are seam/contract drift not API
metadata:
  type: project
---

cenvkit v2 rich-provenance plan (`docs/superpowers/plans/2026-06-16-cenvkit-rich-provenance.md`)
Task 2 engine code: EVERY compose-go v2.11.0 call verified signature-correct by `go doc` + live `go run` against the pinned module (2026-06-16).

**Why:** the review dimension was "compose-go API fidelity" and the worry was wrong signatures / wrong types. Reverified clean so future reviewers don't re-probe:
- `loader.LoadConfigFiles(ctx, []string, workingDir, ...func(*Options)) (*types.ConfigDetails, error)` — does NOT take env; setting `details.Environment` after is correct.
- `loader.LoadModelWithContext(ctx, types.ConfigDetails, ...func(*Options)) (map[string]any, error)`; Options has SkipInterpolation/SkipValidation/SkipConsistencyCheck/SkipResolveEnvironment (also SkipNormalization, NOT set by plan — and proven NOT needed: with SkipInterpolation the `ports` leaf survives as string `${WEB_PORT}:80`).
- `types.Mapping` IS `map[string]string` → `types.Mapping(chainEnv)` valid. `ConfigDetails.Environment` is `types.Mapping`.
- `ServiceConfig.Environment` is `MappingWithEquals` = `map[string]*string` (plan's *string deref correct). `EnvFile.Path` is string; abs with `WithResolvedPaths(true)`.
- `template.ExtractVariables(map[string]any, *regexp.Regexp)`, `template.DefaultPattern` is *regexp.Regexp var, `template.SubstituteWithOptions(string, Mapping, ...Option)`, `template.WithoutLogging` is an Option, `template.Mapping` is `func(string)(string,bool)`.
- `dotenv.ReadFile(path, nil)` OK (nil LookupFn); returns map[string]string. NOTE: emits a stderr warning for unset external `${VAR}` referenced inside an env_file (docker-compose parity, but a stderr leak for `--json`).
- `cli.WithoutEnvironmentResolution` is bare `func(*ProjectOptions) error` passed without parens (matches engine.go:66); + WithoutEnvironmentResolution proven to keep `svc.Environment` INLINE-ONLY (C separability holds).

**How to apply:** the API surface is sound. The plan's real risks are CONTRACT/SEAM drift, not API: (1) spec §5 declares `engine.ProvenanceFacts`/`provenance.Build` + a `Source.Layer` enum incl `interp`; the plan drops both and builds the Report inside engine — see [[provenance-plan-spec-drift]]. (2) A-attribution `Source.Layer` is taken verbatim from the caller's ProvFile.Layer (all "layer1"/"layer2"), while C invents "env_file"/"environment" and the spec also lists "environment"/"interp" — the layer vocabulary is inconsistent across A vs C vs spec. (3) the seam-check command in T2 Step 6 is the CORRECTED form (no `-deps`, restricted to `$MOD/internal/engine `) — verified `seam OK` on disk; supersedes [[seam-check-go-list-deps-false-positive]].
