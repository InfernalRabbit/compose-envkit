---
name: thin-engine-compose-owns-resolution
description: cenvkit is thin — docker compose owns env_file resolution + variable precedence; cenvkit only assembles the file list, never re-engineers precedence
metadata:
  type: feedback
---

When designing cenvkit's env assembly, do NOT make cenvkit reimplement or "game"
Docker Compose's variable-precedence semantics. The real `docker compose`
resolves `env_file:` contents and computes last-wins precedence over the
`COMPOSE_ENV_FILES` list. cenvkit's only job is to **assemble the ordered file
list** (Layer-1 chain order, secrets last *within the chain*, then enumerated
Layer-2 paths) and hand it to compose; compose decides the rest.

**Why:** User correction (2026-06-15). During the cenvkit plan review, a reviewer
finding (W3) proposed that cenvkit reorder `.secrets.env` to AFTER Layer-2 so a
service `env_file:` could never override a secret var. The user pushed back:
"резолвингом env_file занимается системный композ" — env_file resolution and
variable precedence are docker compose's responsibility. Reordering to force
precedence is exactly the kind of upstream-semantics reimplementation the "thin /
upstream-first" principle forbids. I (architect) was about to over-engineer it.

**How to apply:**
- "secrets last (last-wins)" is a guarantee *within the Layer-1 chain* (cenvkit
  controls chain order) — NOT a cross-layer guarantee. Cross-layer collisions
  (a service env_file redefining a chain/secret var) resolve via compose's
  last-wins over `COMPOSE_ENV_FILES`; **document** that ("don't reuse secret var
  names in service env_files"), don't re-order around it.
- Same lens for env-debug: for resolved/defaulted values, delegate to
  `docker compose config` (`--effective`); cenvkit's own `--value` returns raw
  last-wins literals (documented v1 narrowing), it does not expand `${VAR:-def}`.
- LEGITIMATE cenvkit-side work that is NOT reimplementation: correctly *driving*
  compose-go (e.g. compose-go's `WithConfigFileEnv` does not interpolate
  `${COMPOSE_ENV}` inside `COMPOSE_FILE`, so cenvkit must interpolate+split the
  COMPOSE_FILE *selector* itself to know which compose files to enumerate). That
  is compose-file selection, not env_file/variable-precedence resolution — keep it.
- Reinforces the existing CLAUDE.md "upstream-first" rule; this is the concrete
  precedence-engineering instance of it. See also [[cenvkit-go-rewrite-direction]].
