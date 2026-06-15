---
name: spec-circular-interpolation-class
description: cenvkit's reason-for-existing is a compose env_file/interpolation circularity — watch any engine design that re-derives env_file via a single load pass
metadata:
  type: project
---

cenvkit exists because a service `env_file:` value is NOT in compose's
interpolation context (`docs/concepts.md:107-133`, docker/compose#3435 open since
2016). The kit re-feeds env_file paths via `COMPOSE_ENV_FILES` so a 2nd
`docker compose` load can interpolate them.

**Why:** The Go rewrite (§4 of the rewrite spec) routes Layer-2 env_file
*discovery* through compose-go's loader. That newly introduces a cycle the legacy
awk parser never had: the legacy parser only substitutes `${COMPOSE_ENV}` into
env_file paths (`lib/parse-compose-env-files.sh:53-54`), so discovery never
depends on Layer-2 values. A real loader can hit "Layer-2 path needs a Layer-2
value" — the exact class the tool fixes.

**How to apply:** When reviewing `internal/engine`, check (1) the env_file
enumeration pass seeds the load env with what — Layer-1 only, or a fixpoint?
(2) whether the concrete compose-go field holding env_file entries is cited to a
real `file:line` (it is the engine's whole contract; an assumed API = no "no-glob
/ include-aware" guarantee). See `.claude/artifacts/spec-audit.md` C1+C2.
Related: [[carried-bug-classes-cenvkit]].
