---
name: layer2-debug-only-not-populated
description: 2026-06-17 reversal — cenvkit run path no longer folds service env_file: into COMPOSE_ENV_FILES; Layer 2 is debug-only (gap-detector). Headline value-prop flips.
metadata:
  type: project
---

**Decision (2026-06-17, user/owner): drop Layer-2 injection from the run path.**
`cenvkit compose …` and `cenvkit env-files` now populate `COMPOSE_ENV_FILES`
with **Layer 1 only** (the project chain `.env`/`.${ENV}.env`/`.secrets.env` +
tokens + COMPOSE_FILE overlay). A service's `env_file:` is **runtime-only**
(native Docker semantics, per-service isolation) — it is NOT folded into the
compose-time interpolation context anymore.

Layer-2 enumeration is **retained, but only inside `env-debug`**, repurposed from
"simulate the injection" to **gap-detector**: env-debug shows each service's
accurate native runtime env, AND warns when a `${VAR}` in the YAML is satisfied
*only* by a service `env_file:` → at the real run it falls back. env-debug must
**never report a resolution the real run won't produce** (the old `--trace`
`winner: …(layer2) → ports[0]=18080:80` output becomes a lie under this model and
must be redesigned).

**Why:** User judged the old default "illogical" — `env_file:` is semantically
per-service, but folding every service's env_file into one flat
`COMPOSE_ENV_FILES` collapses a shared key (`${PORT}`) into a single global
interpolation value across all services (collision footgun). A per-service
interpolation scope is impossible in a thin/upstream-first tool (compose
interpolates the whole YAML against ONE global env map before splitting per
service), so the real choice was global-inject vs runtime-only — user chose
runtime-only for the run, debug-only for the enumeration.

**How to apply:**
- This REVERSES the headline value-prop. Was: "cenvkit closes docker/compose#3435
  (env_file → interpolation)." Now: "cenvkit manages the Layer-1 chain + a
  daemon-free debugger that *surfaces/diagnoses* the #3435 gap, without latching
  it at run time." Spec + `docs/guide.md` + `docs/cenvkit.md` all need rewriting
  (architect zone).
- Blast radius (code = go-engineer zone): `internal/engine` (stop emitting
  Layer-2 into the run file list; keep enumeration for provenance), the
  `env-files` command (Layer-1 only), `env-debug`/`internal/provenance`
  (gap-detector reframe). `internal/chain` Layer-1 logic is unaffected.
- This is an **engine-contract change → plan-gated**: fresh plan-mode go-engineer
  produces a read-only impl plan against the rewritten spec; architect approves
  before any code. Then qa + code-review, then architect git surgery.
- Open details to settle in the plan (not yet user-decided): is the gap a warning
  (exit 0) or can `cenvkit validate` flag it non-zero? version/back-compat note
  for the behavior break.
- Supersedes the "then enumerated Layer-2 paths" half of
  [[thin-engine-compose-owns-resolution]] for the RUN path; the don't-reimplement-
  precedence principle there still holds for Layer 1. See also
  [[cenvkit-go-rewrite-direction]].
