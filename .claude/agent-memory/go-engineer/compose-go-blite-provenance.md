---
name: compose-go-blite-provenance
description: Proven v2.11.0 B-lite mechanism (var->service/field->resolved) + per-service env(C) separability gotcha + Substitute warning trap
metadata:
  type: project
---

B-lite provenance mechanism for `internal/engine`, PROVEN against compose-go v2.11.0 via live probe 2026-06-16 (artifact: `.claude/artifacts/compose-go-provenance-probe.md`).

**Why:** v2 `env-debug` needs, per `${VAR}` ref, (service, field, resolved value) with NO source coords; spec §6 sketched a two-load diff, but the live probe found a cleaner, single-load mechanism and a normalization trap that would have produced wrong output.

**How to apply:** when implementing `engine.Provenance`, use the mechanism below; do NOT use the naive "read interpolated dict at the same path" approach.

- **Recommended B-lite:** ONE raw dict load (`loader.LoadConfigFiles` -> set `details.Environment` -> `loader.LoadModelWithContext` with `SkipInterpolation=true`, +Skip Validation/ConsistencyCheck/ResolveEnvironment). Walk the dict (map/slice/string leaf), build dotted+`[i]` paths. At each string leaf: `template.ExtractVariables(map[string]any{"x":leaf}, template.DefaultPattern)` to get ref names; `template.SubstituteWithOptions(leaf, mapping, template.WithoutLogging)` to get the resolved value of that exact field. `mapping` = chain env as `template.Mapping`.

- **TRAP — normalization on interpolation:** an INTERPOLATED load expands short forms (`ports`, list `environment`) into structs / `KEY=VALUE`. So reading the same field path in an interpolated dict gives e.g. `ports[0] = map[published:8080 target:80]`, NOT `"8080:80"`. `SkipNormalization` does NOT prevent this (it happens during interpolation). Hence resolve the RAW leaf string in place instead of a second load.

- **TRAP — Substitute warns to stderr** for `${UNSET}` (no default): use `SubstituteWithOptions(..., template.WithoutLogging)`, never bare `Substitute`.

- **ExtractVariables gives names only, NO field path** — keyed by var name -> `Variable{Name,DefaultValue,PresenceValue,Required}`. Walk the dict yourself for paths.

- **C (per-service env) separability HINGES on the D1 lever:** with `WithoutEnvironmentResolution` (already at engine.go:66) `svc.Environment` is INLINE-ONLY and env_file stays in `svc.EnvFiles`. WITHOUT it, `svc.Environment` is MERGED with env_file contents and you can't attribute sources. C = parse each `svc.EnvFiles[].Path` via `dotenv.ReadFile` in order (later overrides earlier), then apply inline `svc.Environment` (overrides). `svc.Environment` is `map[string]*string` (nil-safe).

- `loader.LoadConfigFiles` does NOT take env as an arg — set `details.Environment` before `LoadModelWithContext`. Pass `template.DefaultPattern` (not nil) to `ExtractVariables`. dotenv/template/loader are sub-packages of the pinned module (no new dep) and MUST stay behind `internal/engine` (CI seam).

See [[compose-go-api-facts]], [[d1-lenient-enumeration-lever]].
