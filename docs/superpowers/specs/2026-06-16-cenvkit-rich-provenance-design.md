# cenvkit v2 — rich provenance (`env-debug`) design

Status: **approved direction (brainstorming), 2026-06-16.** Builds on the shipped
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

- **A — chain attribution.** For each variable in the merged `COMPOSE_ENV_FILES`:
  the files that set it (in order), the winner (last-wins), and the shadowed ones.
- **B-lite — interpolation effect.** For a variable: where its `${VAR}` reference
  took effect in the resolved compose (which service / field) and the resolved
  value. **No source coordinates** (no compose-file/line for the `${VAR}` site —
  that is B-full, deferred §8).
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

## 4. Architecture (Approach 1 — engine owns all compose-go)

```
cmd/cenvkit (env-debug)
   │  chain.Resolve(dir) ─────────────► Files, Vars, chain attribution (A, pure Go*)
   │  engine.Provenance(ctx, Input) ──► engine.ProvenanceFacts   (compose-go-free)
   ▼
internal/provenance.Build(chainAttr, facts) ──► Report ──► render: human | --json
```

- **`internal/engine`** (the ONLY compose-go importer; CI-enforced). New method on
  the `Engine` interface: `Provenance(ctx, Input) (ProvenanceFacts, error)`. Does
  all compose-go work — the two loads, dotenv parsing, `template.ExtractVariables`
  — and returns a **compose-go-free** `ProvenanceFacts`.
- **`internal/chain`** — adds chain attribution (A): which file set each key, in
  order, and the winner. *Uses compose-go's dotenv via an engine-exposed helper so
  the parser matches what is reported (see §6 note); the seam stays intact because
  the helper lives in `engine`.*
- **`internal/provenance`** (new, pure Go, no compose-go) — defines the `Report`
  model, builds it from chain attribution + `ProvenanceFacts`, and renders
  (human text default; `--json`).
- **`cmd/cenvkit`** — `env-debug` wires the three together.

`Provenance` changes the `engine.Engine` contract (a sensitive seam) → the engine
implementation is done via a **plan-mode review** at execution time.

## 5. Data model (serializes to `--json`)

```go
// package provenance
type Source     struct { File, Layer string } // Layer: layer1|layer2|environment|interp
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

```go
// package engine — compose-go-free output of Provenance
type ProvenanceFacts struct {
    Services []ServiceEnvFacts          // C: per-service env + source file per key
    Effects  map[string][]EffectFact    // B-lite: var -> []{service, field, resolved}
}
type ServiceEnvFacts struct { Service string; Entries []KVSource }
type KVSource         struct { Key, Value, SourceFile, Layer string }
type EffectFact       struct { Service, Field, Resolved string }
```

## 6. Data flow (the B-lite mechanism)

1. `chain.Resolve(dir)` → `Files`, `Vars`, and per-var attribution (A) by
   `dotenv.Parse`-ing each `COMPOSE_ENV_FILES` entry in order (last write wins;
   record shadowed sources).
2. `engine.Provenance(ctx, Input{ProjectDir, Env: Vars, ConfigFiles, Profiles})`
   — mechanism **verified** against compose-go v2.11.0
   (`.claude/artifacts/compose-go-provenance-probe.md`):
   - **One RAW (non-interpolated) load:** `loader.LoadConfigFiles(ctx, configFiles,
     workingDir)` → `*types.ConfigDetails`; set `details.Environment` = the chain
     env; then `loader.LoadModelWithContext(ctx, *details, {SkipInterpolation,
     SkipValidation, SkipConsistencyCheck, SkipResolveEnvironment})` → a raw
     `map[string]any` dict where `${VAR}` literals survive.
   - **B-lite:** *walk* the raw dict to `(field-path, string-leaf)`; at each leaf,
     `template.ExtractVariables(map[string]any{"x": leaf}, template.DefaultPattern)`
     gives the referenced var NAMES (names only — no path; that is why we walk), and
     `template.SubstituteWithOptions(leaf, mapping, template.WithoutLogging)`
     resolves the leaf *in place* (`mapping` = lookup over the chain env). Emit
     `EffectFact{service, field, resolved}` per name. **Do NOT** read the resolved
     value from an interpolated load — interpolation expands short forms (`ports:
     "8080:80"` → a struct): the *normalization-on-interpolation trap*. One raw
     load + per-leaf substitute avoids it entirely.
   - **C:** relies on the engine's existing `cli.WithoutEnvironmentResolution` (the
     D1 lever), which keeps `ServiceConfig.Environment` INLINE-ONLY and leaves the
     `env_file:` list separate in `svc.EnvFiles`. Per service: `dotenv.ReadFile`
     each `svc.EnvFiles[].Path` in declaration order (later overrides earlier), then
     apply inline `svc.Environment` (`map[string]*string`, overrides), recording the
     source of each final key → `ServiceEnvFacts`. (If the D1 lever were dropped,
     `Environment` would be env_file-merged and C would be unattributable.)
3. `provenance.Build(chainAttr, facts)` → `Report`.
4. Render: human tree/table by default; `--json` emits `Report`.

> §6 note (parser consistency): A's attribution and C's parsing both go through
> compose-go's `dotenv` so the *reported* sources match docker-compose semantics
> exactly. The helper is exposed from `engine` (e.g. `engine.ParseDotEnv(path)`)
> so `chain` can use it without importing compose-go directly — seam intact. v1's
> hand-rolled `chain.parseDotEnv` (still used to build the interpolation seed) may
> be unified onto this helper as a follow-up; not required for v2.

## 7. CLI surface (extends `env-debug`; all daemon-free)

- `cenvkit env-debug --trace VAR [--json]` → `VarTrace`: value, winner file,
  shadowed files (in order), and effects (service/field/resolved). **[A + B-lite]**
- `cenvkit env-debug --effective [--service S] [--json]` → per-service env with
  sources; `--service` filters to one. **[C]**
- `cenvkit env-debug --chain [--json]` / `--files [--json]` → as v1, plus JSON.
- `cenvkit env-debug --value VAR` → the winning value from provenance (replaces
  v1's raw-merge `--value`).

## 8. Non-goals (this increment)

- **B-full** — source coordinates (compose file + field/line for each `${VAR}`
  site). Deferred; needs raw-YAML position tracking across the include graph.
- **Two-pass / fixpoint `env_file:`-path resolution** (v1 §4a limitation) — still
  deferred; separate increment.
- **Rendered-compose artifact** (`render`/cross-env diff) — separate increment.
- Plugin system; `TF_VAR_*`; pnpm/yarn wrappers (out of scope, per v1 §11).

## 9. Error handling (consistent with v1 §9)

- **No compose file** (`HasComposeFileEnv` false, G4) → chain-only `Report`
  (`Vars` with A; empty `Services`/`Effects`); not an error.
- **Malformed compose / load failure** → fatal, actionable, wrapped error.
- **Missing chain file** → skipped (parity); simply not a source.
- **`--trace VAR` for an unset var** → reported "not set" (empty), not an error.

## 10. Testing

- **`provenance.Build` (unit, qa):** table-driven over fake `ProvenanceFacts` +
  chain attribution — winner/overridden ordering, Effects mapping, ServiceEnv
  sources. Pure Go, no docker.
- **`engine.Provenance` (unit):** over `examples/monorepo` (service `env_file`s
  exist on disk) + scratch fixtures — assert the raw-load dict walk attributes a
  real var used in a field (a `ports:` `${VAR}` → service/field/resolved) and that
  per-service sources are correct. No docker.
- **`--json` golden tests** — stable schema.
- **Acceptance:** new `env-debug --trace`/`--json` assertions. These are **new**
  assertions → the acceptance count grows beyond N=60; the new exact total is
  pinned in the v2 plan (lead sign-off).
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
