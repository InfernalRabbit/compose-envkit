# cenvkit v3 — Layer 2 becomes debug-only (run path drops env_file injection)

Status: **design, 2026-06-17 (owner-confirmed direction; implementation
plan-gated).** Supersedes the run-path env-file model of the two prior specs:

- `2026-06-15-cenvkit-go-rewrite-design.md` — §2 ("thin = assemble
  COMPOSE_ENV_FILES … exec"), §4 step 3 + step 5, §4c, §13 acceptance items tied
  to env_file→interpolation. **Superseded for the run path.**
- `2026-06-16-cenvkit-rich-provenance-design.md` — §2, §6 (the B-lite "merged
  effective env" resolution that makes a Layer-2-only `${WEB_PORT}` resolve to its
  real value), §7 `--trace` output. **Superseded for env-debug semantics.**

Everything NOT about folding service `env_file:` into the interpolation context
(Layer-1 chain, COMPOSE_FILE selection/interpolation, include-graph awareness, the
engine seam, dual-mode distribution, `init`, determinism, the dotenv/template
parser reuse, the D1 lever, the daemon-free in-process design) **carries over
unchanged**.

Companion delta (rationale, before/after table, UX): `.claude/artifacts/2026-06-17-layer2-debug-only-design-delta.md`.

---

## 1. The decision

`env_file:` declared under a service is **runtime-only** — exactly Docker's native
semantics. cenvkit no longer folds it into the compile-time interpolation context.

- **Run path** (`cenvkit compose …`, `cenvkit env-files`): `COMPOSE_ENV_FILES` =
  **Layer 1 only** (the project chain). Service `env_file:` paths are NOT appended.
- **Layer-2 enumeration is retained, but only inside `env-debug`**, repurposed
  from "simulate the injection" to **gap-detector**.

### 1.1 Why (the reversal)

Folding every active service's `env_file:` into one flat `COMPOSE_ENV_FILES`
collapses a shared key into a single **global** interpolation value across all
services (`${PORT}` resolves to one value project-wide — a collision footgun),
because compose interpolates the whole YAML against ONE global env map *before*
splitting per service. A per-service interpolation scope is impossible in a thin,
upstream-faithful tool (it would require forking compose-go's interpolation —
forbidden by the upstream-first principle). The owner judged global injection
"illogical as a default"; the chosen model is runtime-only at the run, debug-only
for the enumeration.

### 1.2 What cenvkit IS, post-reversal (value-prop flip)

- **Was:** "cenvkit closes docker/compose#3435 (env_file → interpolation)."
- **Now:** "cenvkit manages the Layer-1 env chain and gives a daemon-free debugger
  that **surfaces/diagnoses** the env_file/interpolation gap — without latching it
  at run time."

`cenvkit compose` still earns its place over raw `docker compose`: it assembles
the Layer-1 chain (token substitution `${ENV}`/`${COMPOSE_ENV}`/`${HOST}`/
`${HOSTNAME}`, `.docker-env-chain`, `.secrets.env`-last ordering) and does
`COMPOSE_FILE` selection + `${COMPOSE_ENV}` interpolation + separator split before
exec. None of that changes. Only the Layer-2 append is removed.

## 2. Run-path contract (the behavior change)

| Surface | Before | After |
|---|---|---|
| `cenvkit env-files` | Layer 1 + Layer 2 | **Layer 1 only** |
| `cenvkit compose …` | `COMPOSE_ENV_FILES` = L1+L2, exec | `COMPOSE_ENV_FILES` = **L1 only**, exec |
| `${VAR}` defined only in a service `env_file:` | resolved (gap closed) | **falls back** (native), debugger flags it |
| service `env_file:` at container runtime | per-service inject (native) | per-service inject (native) — **unchanged** |
| `cenvkit validate` | `docker compose config -q` over L1+L2 | over **L1 only** (same mechanism) |

Carried over unchanged: §4 step 1–2 (ENV/HOST resolve, Layer-1 chain), the
`COMPOSE_FILE` selection/interpolation seam (v1 §4 caveat), the include-graph load
(no glob/over-discovery), the D1 lever, W1 chain sanitization, determinism.

The engine **still enumerates** the active Layer-2 env_file set (it's needed for
the gap-detector and `--effective`); it simply is **not emitted into the run file
list**. Concretely: `engine.Resolve` keeps `ProjectView.Services` (service →
env_file abs paths) but its `Result.EnvFiles` (the run list) becomes Layer-1-only
— or `cmd` stops appending the engine's Layer-2 to the chain when building
`COMPOSE_ENV_FILES`. The plan picks the exact lever; the **contract** is: nothing
a service `env_file:` defines reaches `COMPOSE_ENV_FILES`.

## 3. env-debug — gap-detector model

`env-debug` now reasons over **two distinct env contexts** and must never report a
resolution the real run won't produce:

1. **Interpolation env** (what the real run interpolates against):
   host/shell env + **Layer-1 chain only**. This is exactly the new
   `COMPOSE_ENV_FILES` plus the process env. NO service env_files.
2. **Per-service runtime env** (native, per container): for service S,
   `dotenv.ReadFile(svc.EnvFiles)` in declared order (later wins) + inline
   `environment:` last (inline overrides env_file). This is `--effective` — the
   **final values that land in the container**. **v3 correction (D5):** the inline
   `environment:` `${VAR}` interpolation MUST use the **Layer-1-only interpolation
   env** (`interpEnv`), NOT the all-layers env — because at the real run inline
   `environment:` interpolates against `COMPOSE_ENV_FILES` (= L1) + host, never
   against a service `env_file:`. env_file *literal* entries are injected verbatim
   (unaffected). So if an inline `environment: FOO: "${X}"` references an
   env_file-only `X`, `--effective` shows the **fallback** (matching the run), not
   the env_file value. (This reverses v2 §6 C, which fed the C-load the merged
   L1+L2 env.)

### 3.1 The gap condition

A **gap** for variable `V` at a reference site = `${V}` is referenced in the YAML
(a service field) **AND** `V` is NOT resolvable from the interpolation env (so the
run uses the leaf's `:-default`/empty) **AND** `V` IS defined in at least one
active service's `env_file:` (Layer-2 set). That triple is precisely the
docker/compose#3435 footprint. (If `V` is unset everywhere it is just an
ordinary unresolved fallback, optionally reported, but **not** flagged as a gap.)

> Nuance to surface in output: the env_file value applies only to *its* service's
> container, even if `${V}` is referenced in a different service — so "it's right
> there in web/.web.env" still does not feed `${WEB_PORT}` at the run. That is the
> whole point of the diagnostic.

### 3.2 Mechanism (recombines verified v2 pieces — low feasibility risk)

Both halves already exist in the v2 probe (`.claude/artifacts/compose-go-provenance-probe.md`);
this increment **recombines** them, it does not invent new compose-go usage:

1. **Interpolation env (only L1 + shell):** `dotenv.ReadFile` each **Layer-1**
   file in chain order (later wins) + overlay shell `Env` last. (Drop the v2 step
   that also folded Layer-2 into this merged env.)
2. **One RAW (non-interpolated) load** → walk the dict to `(field-path,
   string-leaf)`; `template.ExtractVariables` gives referenced var NAMES per leaf
   (unchanged).
3. **Effect resolution = the REAL run value:** `template.SubstituteWithOptions(leaf,
   mapping=interpolationEnv, WithoutLogging)`. Because `mapping` is now L1-only, a
   Layer-2-only `${V}` resolves to its **fallback** — matching the run. (This is
   the core change from v2 §6, which used the merged L1+L2 mapping.)
4. **Per-service runtime defs (gap evidence):** for each active service,
   `dotenv.ReadFile(svc.EnvFiles)` (the C parse, already needed for `--effective`).
   Build a map `V → [{service, file, value}]` of env_file definitions.
5. **Gap flag:** for each Effect site referencing `V`: `Gap = (V not in
   interpolationEnv) && (V in the per-service-defs map)`.

### 3.3 Data model deltas (`internal/provenance.Report`)

Carry over `Source`, `Effect`, `EnvEntry`, `ServiceEnv`, `Report.Files/Services`.
Changes (final field names are the plan's to lock; the **semantics** are the
contract):

- `Report.Files` = the new `COMPOSE_ENV_FILES` = **Layer 1 only** (was L1+L2).
  **Keep the existing `ChainFiles` field** (do not delete it) — post-v3 `Files`
  equals `ChainFiles`; retaining both avoids churn in the `--chain` path. The
  runtime-only L2 set for `--files` comes from `Report.Services`/`ProjectView`
  (see §3.4 `--files`).
- `Effect` gains a `Gap bool` (and its `Resolved` is now the **real run value**,
  i.e. the fallback when the var is env_file-only — not the merged-env value).
- `VarTrace` gains the runtime side and the gap:
  - `InChain bool` — resolvable from the interpolation (L1) env?
  - `Value` / `Winner` / `Overridden` — attribution **over the interpolation env
    only** (L1 chain + shell overlay). A var defined only in a service env_file has
    NO chain winner.
  - `RuntimeDefs []ServiceVal` (`{Service, File, Value}`) — service `env_file:`
    definitions of the var (runtime-only; the gap evidence). May hold differing
    values per service.
  - `Gap bool` — `referenced-in-YAML && !InChain && len(RuntimeDefs) > 0`.

### 3.4 CLI surface (extends v2 §7; all daemon-free)

- `--trace --var V [--json]` — reframed (no longer claims a layer2 winner):
  ```
  $ cenvkit env-debug --trace --var WEB_PORT
  WEB_PORT
    interpolation: NOT in the Layer-1 chain -> ${WEB_PORT} falls back at run time
    runtime:       web/.web.env -> WEB_PORT=18080  (service `web` container env only)
    ⚠ gap: ${WEB_PORT} used in service web ports[0] ("${WEB_PORT:-0}:80") resolves
           to :-0 at the run, NOT 18080 (defined only in a service env_file).
    fix:   add WEB_PORT to the Layer-1 chain (e.g. .env), or use it runtime-only.
  ```
  When `V` IS in the chain: normal winner/overridden/effects, no gap line.
- `--effective [--service S] [--json]` — **unchanged** (native per-service env [C]).
- `--chain [--json]` — unchanged (the Layer-1 chain, secrets last).
- `--files [--json]` — **repurposed** (decision 2026-06-17, §8 D2): prints TWO
  labeled groups so the new model is legible at a glance —
  (1) **interpolation** (`COMPOSE_ENV_FILES`, = Layer 1) and
  (2) **runtime-only** (service `env_file:` paths, grouped by service, marked
  "NOT interpolated — container env only"). This keeps `--files` distinct from
  `--chain` and reinforces the runtime-vs-interpolation split. The runtime-only
  group is derived from `Report.Services`; `--json` carries both groups.
- `--value --var V` — winning value from the **interpolation** env (empty if
  env_file-only; the gap is visible via `--trace`).

### 3.5 The gap is a warning, not an error (this increment)

`env-debug` reports the gap informationally and exits **0**. A strict gate
(`cenvkit validate --strict-interp` failing non-zero on a detected gap) is a
**deferred follow-up** (§6) — owner steer 2026-06-17: warn now, strict gate later.

## 4. Code change surface (zones; plan finalizes levers)

- **docs (architect, done alongside this spec):** rewrite `docs/guide.md`
  §1/§4/§7/§9/§10 + `docs/cenvkit.md`; flip the value-prop; move "Layer 2" out of
  the merged-list section into env-debug.
- **internal/engine (go-engineer):** stop emitting Layer-2 into the run file list
  (keep `ProjectView` enumeration for provenance). Change the B-lite interpolation
  env to L1-only; add per-service-defs gathering + gap flagging into `Provenance`.
- **cmd/cenvkit (go-engineer):** `env-files`/`compose` build `COMPOSE_ENV_FILES`
  from Layer 1 only; `env-debug` renders the new gap fields.
- **internal/provenance (go-engineer):** `Report` model deltas (§3.3) + renderer
  (human gap line + `--json` shape); pure Go.
- **internal/chain (go-engineer):** unaffected.
- **tests (qa):** invert the env_file→interpolation acceptance (now fallback at
  run); new gap-detector unit + acceptance assertions; `--files`/`env-files` now
  L1-only; W3 becomes "Layer-2 paths are absent from COMPOSE_ENV_FILES."

## 5. Acceptance deltas (replaces the env-file-injection items)

1. **Run path:** `cenvkit env-files` and the `COMPOSE_ENV_FILES` set by
   `cenvkit compose` contain **no** service `env_file:` path (Layer-1 only). RED on
   the current (L2-appending) impl.
2. **Inverted #3435:** a `${VAR}` defined only in a service `env_file:` renders to
   its `:-default` under `cenvkit compose config` (native fallback) — the v1
   "resolves instead of falling back" assertion is **inverted**.
3. **Gap-detector:** `env-debug --trace --var V` on the env_file-only case reports
   `Gap=true`, the runtime def (service/file/value), and the real (fallback)
   interpolation value; `--json` carries the gap fields. RED on a non-gap-aware impl.
4. **No false gap:** a var present in the Layer-1 chain reports `Gap=false` and a
   normal winner; a var unset everywhere is not flagged as a gap.
5. **`--effective` unchanged:** per-service native env still accurate.
6. The smoke-monorepo assertion count is re-pinned in the v3 plan (the v2 "68"
   count shifts: inverted #3435 assertions + new gap assertions; net delta locked
   in the plan with lead sign-off, header comments in
   `test/cenvkit-acceptance_test.go` bumped in the same commit).

## 6. Non-goals / deferred (this increment)

- **Strict interpolation gate** (`validate` exits non-zero on a detected gap) —
  deferred follow-up (§3.5). Warn-only now.
- **Auto-promote** (a flag to opt a specific env_file/var back into interpolation)
  — explicitly NOT built; the owner chose runtime-only, not opt-in. Revisit only
  on a new owner decision.
- Everything deferred by the prior specs (B-full source coords, two-pass
  env_file-path resolution, rendered-compose artifact, plugins, `TF_VAR_*`) stays
  deferred.
- **FIXED in v3 (review NIT N-3, 2026-06-17 — briefly deferred, then reinstated):**
  the `--files` runtime-only group must derive its paths from each service's
  DECLARED `env_file:` list (`svc.EnvFiles` → a `ServiceEnv.EnvFiles` field on the
  Report), NOT from C entries. Deriving from C entries dropped any env_file whose
  every key is inline-`environment:`-overridden. **Reinstated because it manifests
  in the canonical `examples/monorepo` blueprint:** `web/.web.env` (only var
  `WEB_PORT`, inline-overridden to 0) vanished from `--files` while `--trace` still
  cited it — a visible inconsistency in the primary documented example. Fix: render
  the runtime-only group from declared env_file paths; qa adds a guard test (a
  fully-overridden env_file still appears in `--files`).

## 7. Risk / upstream-fidelity

- **Feasibility:** low risk — the gap-detector recombines two already-probe-
  verified v2 mechanisms (raw-load dict walk + per-leaf substitute; per-service
  `dotenv.ReadFile`). The only new logic is the L1-only mapping and the gap
  cross-check (pure Go set logic). No new compose-go API surface.
- **Plan-mode review required** (engine-contract change): a fresh plan-mode
  go-engineer produces a read-only implementation plan against THIS spec; architect
  approves before any code (per CLAUDE.md risk gate).
- **compose-go bump checklist** (carry over from v1 §10 / v2 §11): unchanged — the
  COMPOSE_FILE seam, the non-interpolated load, `ExtractVariables`, `dotenv` surface.
- **Behavior break:** this is a deliberate, breaking behavior change vs the shipped
  v1/v2. Pre-1.0 / active rewrite, so acceptable; the plan notes the version/CHANGELOG
  entry so migrants know `${VAR}`-from-env_file no longer resolves at the run.

## 8. Plan-review decisions (2026-06-17, architect sign-off)

Resolutions on the plan-mode review (plan: `.claude/artifacts/2026-06-17-layer2-plan.md`):

- **D1 (was plan R1):** drop the now-dead `engine.Resolve` call from the run path
  (`cmd/cenvkit assemble()`); the run path needs neither the Layer-2 list nor
  `ProjectView`. `env-debug` still calls `engine.Provenance` (which enumerates
  internally). Faster `compose`/`env-files`.
- **D2 (was plan R2):** `--files` is **repurposed** to the two-group view (§3.4),
  NOT collapsed to equal `--chain`. Keep both `Report.Files` (L1-only) and
  `ChainFiles`.
- **D3 (acceptance count): RATIFIED N=75** (architect, 2026-06-17 at integration).
  Plan §5 arithmetic was `68 −1 +5 = 72`; the additional **+3** are the `prov-6`
  `TestProvenance_Effective_InlineEnvInterpolation` (a/b/c) invariants added by the
  D5 follow-up directive (the `--effective` inline-`${env_file_var}` fallback test),
  which postdated the plan's 72. So `68 −1 +5 +3 = 75` is the legitimate final
  count. qa reconciled the stale `72`/`68`/`60→68` count comments to 75 in the test
  file. (Caveat per review: "75" is a counted PASS-site figure, not the `go test`
  func count.)
- **D4 (was plan R4):** `test/seam_test.go` STAYS as the engine-enumeration
  contract (engine still enumerates the active L2 set — the debugger depends on
  it); ADD a cmd-level test asserting the run path builds `COMPOSE_ENV_FILES`
  Layer-1-only. qa owns the exact shape.
- **D5 (plan correction, owner-driven 2026-06-17):** the `--effective` (C) load
  must interpolate inline `environment:` against the **Layer-1-only** interpolation
  env (`interpEnv`), NOT the all-layers `chainEnv`. The plan §3 originally said
  "keep chainEnv for C, intentional and correct" — that was **wrong**: it would make
  `--effective` show an env_file-only `${X}` resolving (e.g. `18080`) when the real
  container gets the fallback. Owner requirement: `--effective` shows the TRUE final
  container values. Concretely: the C resolved-load's `cli.WithEnv` is fed
  `interpEnv`; env_file literal entries (`dotenv.ReadFile(svc.EnvFiles)`) and the
  inline-overrides-env_file precedence are unchanged. See §3 point 2.
- **Otherwise no spec corrections required** — the plan's feasibility verdict
  ("implementable as written") stands; the gap-detector recombines verified v2
  compose-go usage (no new API surface; seam invariant preserved). D5 is a
  semantics fix to the C-load env, not a new mechanism.
