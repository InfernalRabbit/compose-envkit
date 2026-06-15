---
name: compose-go-api-facts
description: Verified compose-go v2 API facts for internal/engine (loader path, EnvFiles, gotchas) + how I verified without context7
metadata:
  type: project
---

compose-go API facts for `internal/engine`, verified 2026-06-15 against module source v2.11.0.

**Why:** spec mandates upstream-first on `github.com/compose-spec/compose-go/v2`; the engine is the only package that imports it, so its real surface drives the interface design.

**How to apply:** when implementing `internal/engine`, use these exact (verified) signatures; don't reinvent.

- Module is `.../compose-go/v2` (NOT the legacy non-v2, which v1.20.2 sneaks in transitively).
- Use `cli.NewProjectOptions(configs, opts...)` then `(*ProjectOptions).LoadProject(ctx)`. `cli.ProjectFromOptions` is DEPRECATED.
- `cli.WithConfigFileEnv` makes load include-aware (honors COMPOSE_FILE + resolves `include:`) — kills glob over-discovery.
- `cli.WithResolvedPaths(true)` makes `types.EnvFile.Path` absolute. `cli.WithEnv([]string{"K=V"})` seeds Layer-1 chain for interpolation.
- `Project.Services` = ACTIVE set (profile-inactive go to `Project.DisabledServices`). `Services` is a `map[string]ServiceConfig` → engine MUST sort for deterministic COMPOSE_ENV_FILES.
- `ServiceConfig.EnvFiles []EnvFile`; `EnvFile{Path, Required OptOut, Format}`. `OptOut` defaults TRUE → a missing *required* env_file makes LoadProject ERROR (sh kit was lenient/skip-missing).
- **D1 RULED 2026-06-15 (provisional, parity-affecting, pending USER confirm):** enumeration/assembly pass is LENIENT (skip missing env_file — smoke suite needs it); required-fatal enforcement is left to the real `docker compose` run. Lenient-at-assembly, upstream-at-runtime. **Why:** smoke-monorepo relies on missing-file skip. **How to apply:** do NOT let compose-go's required-fatal abort enumeration — drop non-existent paths ourselves (or skip-validation on the enumeration load).
- **D2 RULED:** pin EXACT v2.11.0; bump deliberately + re-run acceptance.
- **D3 RULED:** only `internal/engine` imports compose-go; `internal/debug`+CLI use a compose-go-free `ProjectView`.
- **Reviewer C1 (env_file path references a Layer-2 var):** compose-go interpolates env_file *paths* using the load env we seed via `cli.WithEnv` (= Layer-1 chain), NOT vars defined inside other services' Layer-2 files. v1 lean = single-pass, Layer-1-only path interpolation (document the limit); two-pass deferred. Needs explicit resolution model in spec.

**Gotcha — verification method:** context7 + serena MCP tools were NOT exposed in my session (only Read/Edit/Write/Bash/Task/SendMessage). I verified the API authoritatively by `go get github.com/compose-spec/compose-go/v2@latest` into /tmp probe + `go doc` on the real package, then cleaned up. This is the fallback when context7 is unavailable — `go doc` on the actual pinned module is the source of truth, never guess signatures.

Full artifact: `.claude/artifacts/compose-go-api.md`. See [[engine-interface-seam]].
