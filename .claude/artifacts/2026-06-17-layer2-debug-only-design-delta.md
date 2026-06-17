# Design delta — Layer 2 becomes debug-only (2026-06-17)

Owner decision, confirmed via two forks:
1. **Run path drops Layer-2 injection** — `cenvkit compose …` / `env-files`
   populate `COMPOSE_ENV_FILES` with **Layer 1 only**. Service `env_file:` is
   runtime-only (native Docker), never folded into the interpolation context.
2. **env-debug = gap-detector** — Layer-2 enumeration is retained, but only to
   *diagnose* the gap, never to simulate an injection the real run won't make.

This **reverses the headline value-prop**: cenvkit no longer "closes
docker/compose#3435" — it manages the Layer-1 chain and gives a daemon-free
debugger that *surfaces* the gap.

---

## New behavior contract

| Surface | Before | After |
|---|---|---|
| `cenvkit env-files` | Layer 1 + Layer 2 (enumerated service env_file paths) | **Layer 1 only** |
| `cenvkit compose …` | sets `COMPOSE_ENV_FILES` = L1+L2, exec | sets `COMPOSE_ENV_FILES` = **L1 only**, exec |
| `${VAR}` from a service `env_file:` | resolved (gap closed) | **falls back** (native) — debugger flags it |
| service `env_file:` at runtime | injected per-service (unchanged) | injected per-service (unchanged) |
| `env-debug --effective` | per-service env (native) | per-service env (native) — **unchanged, still accurate** |
| `env-debug --trace --var V` | "winner: …(layer2) → effect" (now a lie) | **gap-detector** (see below) |

## env-debug gap-detector — UX

`--effective` (accurate native per-service runtime env) stays as-is. `--trace`
is reframed to never claim a resolution the run won't produce:

```
$ cenvkit env-debug --trace --var WEB_PORT
WEB_PORT
  interpolation: NOT in the chain (Layer 1) -> ${WEB_PORT} falls back at run time
  runtime:       web/.web.env -> WEB_PORT=18080  (service `web` container env only)
  ⚠ gap: ${WEB_PORT} is referenced in service web ports[0] ("${WEB_PORT:-0}:80")
         but WEB_PORT is defined only in a service env_file, so the real run
         resolves it to the :-0 fallback, not 18080.
  fix:   add WEB_PORT to your Layer-1 chain (e.g. .env), or use it runtime-only.
```

When `V` IS in the Layer-1 chain, `--trace` shows the normal Layer-1 winner /
shadowed list (no gap). The gap warning fires only on the
defined-only-in-service-env_file case.

Open detail: warning-only (exit 0) vs. a `cenvkit validate` flag that exits
non-zero on a detected gap. Proposed: env-debug warns (exit 0); leave a
`validate --strict-interp`-style gate as a follow-up, not v1 of this change.

## Change surface (zones)

- **docs (architect):** rewrite spec `docs/superpowers/specs/`, `docs/guide.md`
  §1/§4/§7/§9/§10, `docs/cenvkit.md`. Flip the value-prop; §4 "Layer 2 — service
  env_file paths" moves out of the merged-list section into an env-debug section.
- **internal/engine (go-engineer):** stop emitting Layer-2 paths into the run
  file list; keep the enumeration API for provenance/gap-detection.
- **env-files command (go-engineer):** Layer-1 only.
- **internal/provenance + env-debug (go-engineer):** gap-detector logic;
  redesign `--trace` output + `--json` Report shape (the `winner.layer=layer2`
  field semantics change).
- **internal/chain (go-engineer):** unaffected (Layer-1 logic stands).
- **tests (qa):** flip acceptance expectations (`${VAR}`-from-env_file now
  fallback at run; new gap-detector assertions); update `test/` smoke.

## Execution path (plan-gated — engine contract)

1. Architect rewrites the spec to the new model (this delta → spec).
2. **Fresh plan-mode go-engineer** produces a read-only implementation plan
   against the new spec; architect approves (no code before approval).
3. Implement (go-engineer) → tests (qa) → review (code-reviewer) → architect
   verifies on disk → one squashed commit, git surgery by architect.
