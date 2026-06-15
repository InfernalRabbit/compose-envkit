---
name: d1-lenient-enumeration-lever
description: The compose-go v2.11.0 lever that lets the engine enumerate env_file paths despite a missing required env_file (D1 lenient enumeration) — proven empirically
metadata:
  type: project
---

D1 "lenient enumeration" lever, proven against compose-go v2.11.0 (2026-06-15).

**Fact:** to make `LoadProject` succeed even when a `required:` env_file is
MISSING (so enumeration can still emit the paths), pass
**`cli.WithoutEnvironmentResolution`** (a bare `func(*ProjectOptions) error`, no
call parens). It is the public wrapper for `loader.Options.SkipResolveEnvironment
= true` (cli/options.go:378). Equivalent low-level:
`cli.WithLoadOptions(func(o *loader.Options){ o.SkipResolveEnvironment = true })`
— prefer the named option (no `loader` import at call sites).

**Why it works:** `loader.go:606` only runs `WithServicesEnvironmentResolved`
when `!SkipResolveEnvironment`; that step is the ONLY place that `os.Stat`s
env_file paths and errors on a missing required one (`loadEnvFile`,
types/project.go:748). `EnvFiles` is built earlier (normalization) so the paths
survive the skip.

**How to apply (engine impl):**
- Enumeration load: include `cli.WithoutEnvironmentResolution`. `svc.EnvFiles[].Path`
  stays populated for ALL files incl. the missing one (proven). Then `os.Stat`-
  filter to drop non-existent paths ourselves (sh-kit skip-missing parity) before
  assembling `COMPOSE_ENV_FILES`.
- The real `docker compose` exec runs WITHOUT the lever → enforces `required:`
  upstream-faithfully. "Lenient at assembly, upstream at runtime."

**Gotchas:**
- Path INTERPOLATION still runs under the lever (proven: `${VAR}` in an env_file
  path resolves from `cli.WithEnv` Layer-1 env). The lever only skips READING
  env_file contents, not interpolation/normalization/include/path-resolution.
- `SkipValidation` does NOT help (still errors on missing required) — wrong lever.
- `WithDiscardEnvFile` is the WRONG lever — it WIPES `EnvFiles` after resolving.
- Under the lever `ServiceConfig.Environment` is not computed (fine; we read
  `EnvFiles`, not `Environment`).

Full proof + go doc evidence: `.claude/artifacts/compose-go-d1-lever.md`.
See [[compose-go-api-facts]].
