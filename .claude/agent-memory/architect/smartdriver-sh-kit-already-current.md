---
name: smartdriver-sh-kit-already-current
description: 2026-06-17 — decided NOT to migrate/re-source SmartDriver's SH kit; it's already ~v0.2.0 + better-adapted, and compose-envkit's SH is frozen/EOL.
metadata:
  type: project
---

**Decision (2026-06-17): leave SmartDriver's vendored POSIX-sh kit as-is — no
migration, no re-source.** A verbatim re-source of compose-envkit `v0.2.0` would
be churn that *loses* SmartDriver's local adaptations for ~zero functional gain.

Context — SmartDriver is a real consumer of the **pre-split** sh kit:
- Repo: `/Users/infernal_rabbit/Workflow/Big/SmartDriver` (separate repo, branch
  `master`; NOT the off-limits `Access/monorepo`). 3 projects: root (unified
  stack) + `smart_driver_app/` + `directus/`, each with a `.docker-env-chain`,
  driven by root `scripts/` (`docker-wrapper.sh`, `parse-compose-env-files.sh`,
  `env-debug.sh`, `compose.mk`, `env-debug.mk`) + a `./docker` shim. It has its
  own team protocol (`.claude/TEAM.md`, infra-engineer owns the env tooling).

Verified file-by-file (code-only, comments stripped) vs compose-envkit `v0.2.0`:
- `parse-compose-env-files.sh` — **byte-identical** discovery engine.
- wrapper + `./docker` — v0.2.0 is only a **superset** (`${HOST}`/`${HOSTNAME}`
  chain tokens + `COMPOSE_DEPTH`), and SmartDriver's config uses **neither**.
- `env-debug.sh` — **same engine**; SmartDriver adds a `COMPOSE_TLS` debug line
  + Unicode glyphs. `compose.mk` — SmartDriver carries a `COMPOSE_TLS=true` prod
  safeguard (its "infra-arch-1" no-ingress protection) that v0.2.0 lacks.
- All kit files in SmartDriver are **Russian**; v0.2.0 is the English extraction.

compose-envkit's SH is **frozen + EOL**: sh code is byte-identical `v0.2.0`→
`v0.4.0` (never touched in the Go era), deprecated at `v0.3.0`, removed after
`v0.4.0` (commit `15586f1`). So there is **no newer SH to pull and nothing to
track** — the Go `cenvkit` is the only evolving implementation.

**Why:** SmartDriver's sh kit is already at the SH frontier (≈v0.2.0) and better
adapted to SmartDriver than the canonical tag. The premise we started from
("SmartDriver carries a stale pre-split version that needs upgrading") was false.

**How to apply:** If a future request says "migrate SmartDriver to cenvkit" or
"re-source its sh kit from compose-envkit," the **SH path is effectively a no-op**
— don't re-run the full investigation. The only path with a future is the **Go
`cenvkit`**, which for SmartDriver needs a prebuilt **linux/amd64 binary bundled
into the deploy archive** (its prod server is toolchain-free; dev + GitLab CI
also need the binary, e.g. `go install` once it's published — see
[[thin-engine-compose-owns-resolution]]). Re-confirm git state before acting;
this snapshot can go stale.
