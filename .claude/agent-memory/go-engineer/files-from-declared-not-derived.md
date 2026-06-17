---
name: files-from-declared-not-derived
description: --files runtime-only group must render from ServiceEnv.EnvFiles (declared env_file: paths), NOT reconstructed from per-key C entry sources — inline override erases a fully-overridden file
metadata:
  type: feedback
---

The `env-debug --files` runtime-only group must list each service's DECLARED
`env_file:` paths, carried explicitly on `ServiceEnv.EnvFiles` (populated in the
engine C-loop from `svc.EnvFiles[].Path`). Do NOT reconstruct the file list from
the per-key C `Entries` (`Source.Layer=="env_file"`).

**Why:** the C-loop rewrites a key's `Source` to `"(inline environment:)"` when an
inline `environment:` key shadows an env_file key. If EVERY key of an env_file is
inline-overridden, that file has ZERO surviving env_file-sourced entries and
vanishes from an entries-derived list. This is N-3, and it hit the CANONICAL
examples/monorepo: web declares `.web.env` (only key WEB_PORT=18080) +
`.web.dev.env`, and the compose has `environment: WEB_PORT: "${WEB_PORT:-0}"` —
so `.web.env` dropped from `--files` while `--trace --var WEB_PORT` still cited it
(the two views contradicted each other in the primary documented example).

**How to apply:** any "which files does this service load" view derives from
DECLARED paths (svc.EnvFiles), independent of parse success or per-key overrides.
Per-key VALUE/source views (`--effective`) correctly keep the override semantics
(WEB_PORT shows the inline 0, the true container value). The two are orthogonal:
declared-files for `--files`, per-key-sources for `--effective`. Verify against
examples/monorepo (`cenvkit init` a temp copy first — Layer-1 is seeded from
example.*; see [[monorepo-fixture-layer1-needs-seeding]]) that `--files` and
`--trace` AGREE on a service's env_file set. Related: [[v3-layer2-debug-only-gap-detector]].
