# cenvkit v2 — rich provenance (`env-debug`) design

Status: **implemented (v2), 2026-06-16.** Builds on the shipped
v1 (`docs/superpowers/specs/2026-06-15-cenvkit-go-rewrite-design.md`, §11 deferred
"rich mode"). This spec covers the **provenance** increment only; the other two
deferred items (rendered-compose artifact; two-pass `env_file:`-path resolution)
remain out of scope here (§8).

## 1. Motivation

v1 is "thin": assemble `COMPOSE_ENV_FILES`, `exec docker compose`. Its
`env-debug --value/--trace` deliberately returns **raw last-wins literals** (a v1
narrowing) and `--effective` shells out to `docker compose config` (needs the
daemon). When a value is surprising — "why is `APP_PORT` 8080 and not the 3000 in
`.env`?" — there is no first-class answer.

v2 makes `env-debug` **real**: for any variable, *which file set the winning
value, what it overrode, and where it took effect in the compose model* — and for
any service, *its effective environment with the source file of each value*. All
**in-process (no docker daemon)**, with human output by default and `--json` for
tooling. This supersedes v1's raw `--value/--trace`.

## 2. Scope

Three provenance depths, all in:

- **A — chain attribution.** For each variable in the merged EFFECTIVE env
  (`COMPOSE_ENV_FILES` in order PLUS the shell `environment` overlaid last,
  last-wins — mirroring what compose actually interpolates): the files that set it
  (in order), the winner, and the shadowed ones. When the shell env overrides a
  file, the file is reported as shadowed and the winner is the `environment` layer.
  (A is attribution over the merged effective env, not files-only.)
- **B-lite — interpolation effect.** For a variable: where its `${VAR}` reference
  took effect in the resolved compose (which service / field) and the resolved
  value. **No source coordinates** (no compose-file/line for the `${VAR}` site —
  that is B-full, deferred §8). **Effects are reported for service fields only**
  (`services.<name>.<field>`). `${VAR}` references in top-level `networks:`,
  `volumes:`, `configs:`, `secrets:`, and `x-*` blocks are out of scope this
  increment (alongside B-full, §8). Such a reference still appears in A (chain
  attribution) if the var is a `COMPOSE_ENV_FILES`/chain key; only its `Effects`
  entry is omitted.
- **C — per-service environment.** For each service: the effective container
  environment and the source file of each value (`env_file:` entries in order +
  inline `environment:`).

Output: human-readable default **+ `--json`**, one structured model underneath.

## 3. Use existing parsers — no hand-rolled parsing (verified)

compose-go (already pinned, v2.11.0) is the exact code `docker compose` uses; we
reuse it for **parity** instead of writing our own (`go doc`-verified 2026-06-16):

- **`github.com/compose-spec/compose-go/v2/dotenv`** — `Parse(io.Reader)`,
  `ParseWithLookup` (+ `ReadFile`/`UnmarshalWithLookup`). Used for A (parse each
  chain file in order) and C (per-service `env_file:` parse). Replaces v1's
  simplified `chain.parseDotEnv`; exact dotenv parity (quotes, escapes, `export`,
  in-file interpolation).
- **`github.com/compose-spec/compose-go/v2/template`** — `ExtractVariables(configDict
  map[string]any, pattern *regexp.Regexp) map[string]Variable` + the `Variable`
  type (and `Substitute*`). Used for B-lite to find `${VAR}` references in the
  raw (non-interpolated) compose dict. Replaces a hand-rolled `${A:-${B}}`
  tokenizer.

**Seam consequence:** both are `compose-spec/compose-go/...` packages, so the
CI seam check (only `internal/engine` may import compose-go) requires they live in
`internal/engine`. This reinforces Approach 1 below.

## 4. Architecture (Approach 1 — engine owns all compose-go; single `Report`)

```
cmd/cenvkit (env-debug)
   │  chain.Resolve(dir) ───────────────► Files, Vars
   │  assemble ordered EnvFiles (Layer-1 from chain, Layer-2 from engine.Resolve)
   │  engine.Provenance(ctx, ProvInput) ─► provenance.Report   (A + B-lite + C)
   ▼
provenance.RenderHuman | RenderJSON(Report) ──► human (default) | --json
```

- **`internal/engine`** (the ONLY compose-go importer; CI-enforced). New method on
  the `Engine` interface: `Provenance(ctx, ProvInput) (provenance.Report, error)`.
  Does all compose-go work — the raw + resolved loads, `dotenv` parsing,
  `template.ExtractVariables`/`SubstituteWithOptions` — AND assembles chain
  attribution (A, over the cmd-supplied ordered `EnvFiles`), interpolation effects
  (B-lite), and per-service env (C) **directly into a `provenance.Report`**. There
  is no intermediate compose-go-free `ProvenanceFacts` type and no separate
  `provenance.Build` step — the engine returns the finished `Report`.
- **`internal/provenance`** (new, pure Go — imports neither compose-go nor
  `engine`) — defines the shared `Report` model and renders it (human text
  default; `--json`). It is a fast, dependency-light leaf.
- **`cmd/cenvkit`** — `env-debug` calls `chain.Resolve`, assembles the ordered
  `EnvFiles`, calls `engine.Provenance`, and renders the returned `Report`.

**Coupling:** `engine` imports `provenance` for the shared model types (one
direction); `provenance` imports neither. (A is computed inside `engine`, not in
`chain` — `chain` does not import compose-go and does no attribution here.)

`Provenance` changes the `engine.Engine` contract (a sensitive seam) → the engine
implementation is done via a **plan-mode review** at execution time.

## 5. Data model (serializes to `--json`)

```go
// package provenance
type Source     struct { File, Layer string } // Layer: layer1|layer2|env_file|environment
type Effect     struct { Service, Field, Resolved string }
type VarTrace   struct {
    Name       string
    Value      string     // effective (winning) value
    Winner     Source     // file that set the winning value
    Overridden []Source   // earlier files that set it, shadowed, in order
    Effects    []Effect   // B-lite: where ${Name} took effect + resolved value
}
type EnvEntry   struct { Key, Value string; Source Source }
type ServiceEnv struct { Service string; Entries []EnvEntry } // C
type Report     struct {
    Files    []string             // the COMPOSE_ENV_FILES order (context)
    Vars     map[string]VarTrace  // A + B-lite
    Services []ServiceEnv         // C
}
```

`engine.Provenance` returns this `provenance.Report` **directly** — there is no
intermediate engine-side `ProvenanceFacts`/`ServiceEnvFacts`/`KVSource`/`EffectFact`
type and no `provenance.Build` step. The engine assembles A + B-lite + C into the
`Report` in one pass. (`engine` imports `provenance` for these shared types.)

## 6. Data flow (the B-lite mechanism)

1. `chain.Resolve(dir)` → `Files`, `Vars`. `cmd/cenvkit` assembles the ordered
   `EnvFiles` (Layer-1 from the chain `Files`, Layer-2 from `engine.Resolve`) and
   passes them in `ProvInput`. (Attribution itself happens inside `engine` — see
   step 2's A.)
2. `engine.Provenance(ctx, ProvInput{ProjectDir, Env: Vars, ConfigFiles, Profiles,
   EnvFiles})` — mechanism **verified** against compose-go v2.11.0
   (`.claude/artifacts/compose-go-provenance-probe.md`):
   - **Merged interpolation env (first):** `dotenv.ReadFile` each `EnvFiles` entry
     in declaration order (later file wins), then overlay `Env` (OS/Layer-1 seed)
     last → the effective env that drives both the B-lite `mapping` and the C-load.
   - **A — chain attribution (over the merged EFFECTIVE env):** for each var, the
     `EnvFiles` that set it (in order), the winner (last-wins), shadowed sources,
     PLUS a final `(environment)` overlay so the reported `Value`/`Winner`/
     `Overridden` match what compose actually interpolates (when the shell env
     overrides a file, the file becomes Overridden and the winner is
     `{File:"(environment)", Layer:"environment"}`).
   - **One RAW (non-interpolated) load:** `loader.LoadConfigFiles(ctx, configFiles,
     workingDir)` → `*types.ConfigDetails`; set `details.Environment` = the chain
     env; then `loader.LoadModelWithContext(ctx, *details, {SkipInterpolation,
     SkipValidation, SkipConsistencyCheck, SkipResolveEnvironment})` → a raw
     `map[string]any` dict where `${VAR}` literals survive.
   - **B-lite:** *walk* the raw dict to `(field-path, string-leaf)`; at each leaf,
     `template.ExtractVariables(map[string]any{"x": leaf}, template.DefaultPattern)`
     gives the referenced var NAMES (names only — no path; that is why we walk), and
     `template.SubstituteWithOptions(leaf, mapping, template.WithoutLogging)`
     resolves the leaf *in place* (`mapping` = lookup over the **merged** effective
     env from the first step, so a Layer-2-only `${WEB_PORT}` resolves to its real
     value, not a `:-default`). Append an `Effect{service, field, resolved}` to the
     var's `VarTrace.Effects` per name. **Do NOT** read the resolved value from an
     interpolated load — interpolation expands short forms (`ports: "8080:80"` → a
     struct): the *normalization-on-interpolation trap*. One raw load + per-leaf
     substitute avoids it entirely.
   - **C:** relies on the engine's existing `cli.WithoutEnvironmentResolution` (the
     D1 lever), which keeps `ServiceConfig.Environment` INLINE-ONLY and leaves the
     `env_file:` list separate in `svc.EnvFiles`. The resolved load is fed the SAME
     merged effective env via `cli.WithEnv` (so inline `${VAR}` values resolve like
     B-lite). Per service: `dotenv.ReadFile` each `svc.EnvFiles[].Path` in
     declaration order (later overrides earlier), then apply inline
     `svc.Environment` (`map[string]*string`, overrides), recording the source of
     each final key → `ServiceEnv.Entries`. (If the D1 lever were dropped,
     `Environment` would be env_file-merged and C would be unattributable.)
3. `engine.Provenance` assembles A (over the cmd-supplied ordered `EnvFiles`) +
   B-lite + C **directly** into a `provenance.Report` — no separate `Build` call.
4. Render: human tree/table by default; `--json` emits `Report`.

> §6 note (parser consistency): A's attribution and C's parsing both go through
> compose-go's `dotenv` (the engine-internal lowercase `parseDotEnv`, threaded a
> `dotenv.LookupFn` over the merged chain env) so the *reported* sources match
> docker-compose semantics exactly. A is computed inside `engine` (not `chain`), so
> no exported `engine.ParseDotEnv` helper is needed and `chain` does no compose-go
> work. A genuinely-unset external `${VAR}` inside an env_file still warns
> (correct last-wins parity); only chain-provided vars are silenced via the lookup.

## 7. CLI surface (extends `env-debug`; all daemon-free)

- `cenvkit env-debug --trace --var VAR [--json]` → `VarTrace`: value, winner file,
  shadowed files (in order), and effects (service/field/resolved). **[A + B-lite]**
- `cenvkit env-debug --effective [--service S] [--json]` → per-service env with
  sources; `--service` filters to one. **[C]**
- `cenvkit env-debug --chain [--json]` / `--files [--json]` → as v1, plus JSON.
- `cenvkit env-debug --value --var VAR` → the winning value from provenance
  (replaces v1's raw-merge `--value`).
- `--trace` and `--value` are bool flags; the variable is named via `--var VAR`
  (v1 consistency — keeps the existing `--var` flag and committed tests).
- The v1 `--diff` flag is **removed** in v2 (superseded by `--trace --var` +
  `--effective`; see §8).

## 8. Non-goals (this increment)

- **B-full** — source coordinates (compose file + field/line for each `${VAR}`
  site). Deferred; needs raw-YAML position tracking across the include graph.
- **Non-service interpolation effects** — `${VAR}` sites in top-level
  `networks:`/`volumes:`/`configs:`/`secrets:`/`x-*` blocks are not attributed in
  `Effects` this increment (only `services.<name>.<field>` fields are; see §2 B-lite).
- **Two-pass / fixpoint `env_file:`-path resolution** (v1 §4a limitation) — still
  deferred; separate increment.
- **Rendered-compose artifact** (`render`/cross-env diff) — separate increment.
- Plugin system; `TF_VAR_*`; pnpm/yarn wrappers (out of scope, per v1 §11).

## 9. Error handling (consistent with v1 §9)

- **No compose file** (`HasComposeFileEnv` false, G4) → chain-only `Report`
  (`Vars` with A; empty `Services`/`Effects`); not an error.
- **Malformed compose / load failure** → fatal, actionable, wrapped error.
- **Missing chain file** → skipped (parity); simply not a source.
- **`--trace --var VAR` for an unset var** → reported "not set" (empty), not an error.

## 10. Testing

- **`provenance` render (unit, qa):** table-driven over **hand-built `Report`
  fixtures** — `RenderHuman`/`RenderJSON` over winner/overridden ordering, Effects
  mapping, ServiceEnv sources. Pure Go, no docker (no `Build` step exists; the
  engine returns the finished `Report`).
- **`engine.Provenance` (unit):** over `examples/monorepo` (service `env_file`s
  exist on disk) + scratch fixtures — assert the raw-load dict walk attributes a
  real var used in a field (a `ports:` `${VAR}` → service/field/resolved) and that
  per-service sources are correct. No docker.
- **`--json` golden tests** — stable schema.
- **Acceptance:** new `env-debug --trace --var`/`--json`/`--effective`/`--value
  --var` assertions. The smoke-monorepo count **grows 60 → 68** (baseline 60 + 8
  net provenance assertions; the new `--value --var` assertion REPLACES the v1 raw
  `--value` one, so it is net-zero). The exact total (68) is pinned in the v2 plan
  Task 4 Step 2 with lead sign-off, and the header count comments in
  `test/cenvkit-acceptance_test.go` are bumped 60 → 68 in the same commit.
- All provenance tests are **daemon-free**.

## 11. Risk / upstream-fidelity

- **Probe — DONE (2026-06-16):** verified at compose-go **v2.11.0**
  (`.claude/artifacts/compose-go-provenance-probe.md`). Findings that shaped §6:
  the mechanism is a **single raw load** (`loader.LoadConfigFiles` →
  `loader.LoadModelWithContext{SkipInterpolation,…}`) + a dict walk + per-leaf
  `template.SubstituteWithOptions(…, template.WithoutLogging)`;
  `template.ExtractVariables` returns variable NAMES only (no field paths → we
  walk the dict); a two-load diff is WRONG (the normalization-on-interpolation
  trap — interpolation expands `ports`/list-`environment` into structs); and C
  requires the existing `cli.WithoutEnvironmentResolution` lever to keep inline
  `environment:` separable from `env_file:`.
- Add to the v1 spec §10 bump checklist: on a compose-go bump, re-confirm the
  non-interpolated-load + `ExtractVariables` behavior and the `dotenv.Parse`
  surface.
- **Pin discipline:** `dotenv`/`template` are sub-packages of the already-pinned
  module — no new dependency, no version drift beyond compose-go itself.
