# cenvkit — `env-debug --overview` (env layering overview)

Status: **design, 2026-06-17 (owner-approved direction; implementation
plan-gated).** Additive to the v3 model
(`2026-06-17-cenvkit-layer2-debug-only-design.md`); changes no existing behavior.

## 1. Motivation

The removed POSIX-`sh` kit had a `make env-debug-diff` overview that the current
`env-debug` modes don't replace: a **per-file accumulation walk** showing, for each
file in the chain, which variables are new / overridden / repeated, with the raw
(literal) values — plus per-service `env_file:` sections. v1's `--diff` was dropped
in v2 (deemed covered by `--trace` + `--effective`), but those are point lenses:
`--trace` is one variable, `--effective` is resolved per-service values. Neither
shows the **whole layering at a glance** — "what does each file contribute, and
what does it shadow."

`env-debug --effective` already replaces (and exceeds) the sh `env-debug-effective`
mode — it is daemon-free and annotates each value's source — so only the
overview/diff lens is missing. This spec adds it back, correct under v3.

## 2. Scope & lens (owner-confirmed)

A new daemon-free mode `cenvkit env-debug --overview` with a **hybrid lens**: raw
(literal) values + layering markers, plus v3 gap annotations. Two sections:

1. **Interpolation chain (`COMPOSE_ENV_FILES`)** — the **Layer-1 chain only** (what
   actually feeds `${VAR}` interpolation under v3). A per-file accumulation walk in
   chain order with markers:
   - `+` new — the key is first defined in this file.
   - `~` override — the key was set by an earlier chain file; show `old → new`.
   - `·` repeat — the key is set again to the same value.
2. **Runtime-only — service `env_file:` (NOT interpolated)** — per active service:
   its declared `env_file:` paths in order (each a layer with `+/~/·` markers
   relative to a **per-service** accumulator), then a final **`inline environment:`**
   pseudo-layer (inline overrides `env_file:` — the true container precedence),
   also marked. After each service, a `⚠ gap:` line for any var that is referenced
   as `${VAR}` in the YAML but defined only in this service's `env_file:` (so the
   run falls back) — reusing the existing v3 gap detection.

**Values are raw/literal** — exactly as written in the file, with `${...}`
**unexpanded** (e.g. `POSTGRES_USER = ${DATABASE_POSTGRES_USER:-directus}`). This
is the "what each file literally contributes" lens; resolved values are
`--effective`'s job. (Implementation note → §6: the literal-value + declaration-
order requirement must be verified against compose-go's `dotenv` surface; a plain
ordered line parse may be needed rather than `dotenv.Parse` if the latter expands
or reorders.)

**Header** (best-effort, like the sh kit): `COMPOSE_ENV = <value> (from <source>)`
and `Project dir = <dir>`. Source label (shell / `.env` / default) only if cheaply
available from the chain resolution; otherwise show the value + dir alone.

### Example (examples/monorepo, dev)

```
env overview — monorepo (mode: overview)
  COMPOSE_ENV = dev (from .env)
  Project dir = /app

Interpolation chain (COMPOSE_ENV_FILES)
  + new   ~ override   · repeat

  .env
      + COMPOSE_PROJECT_NAME = monorepo
      + COMPOSE_ENV = dev
      + SITE_URL = example.com
  .dev.env
      + IS_DEV = true
      ~ SITE_URL = example.com → dev.example.com

Runtime-only — service env_file: (NOT interpolated)
  web:
    web/.web.env
        + WEB_PORT = 18080
    web/.web.dev.env
        + WEB_DEBUG = true
    inline environment:
        + STACK_TIER = dev
        ~ WEB_PORT = 18080 → ${WEB_PORT:-0}
    ⚠ gap: WEB_PORT — used as ${WEB_PORT} in service web (ports[0],
      environment[0]) but NOT in the Layer-1 chain → run falls back to :-0.
```

## 3. Architecture (holds the engine seam)

- **`internal/engine`** (the only compose-go importer) populates a new ordered,
  raw, per-file structure on the `Report` — chain (Layer-1) files first, then per
  active service its `env_file:` layers + an `inline environment:` pseudo-layer.
  It **reuses the existing Provenance loads only for *discovery*** (which chain
  files, which services, which `env_file:` paths, in what order); the per-file
  **entries are captured by a NEW ordered raw line read** — the existing
  dotenv-parsed `parsed[i]` is unordered AND `${...}`-expanded (probe-confirmed,
  §6), so it cannot feed the literal+ordered lens. The line reader is stdlib-only
  (pure Go) and lives in `internal/engine`, preserving the seam.
- **`internal/provenance`** (pure Go) computes the `+/~/·` markers by walking the
  layers with an accumulator (one for the chain section; a fresh one per service in
  the runtime section) and renders the human output; `--json` serializes the
  structure. Gap lines reuse `Vars[].Gap` / `RuntimeDefs`.
- **`cmd/cenvkit`** adds the `--overview` flag and passes the header inputs
  (`COMPOSE_ENV` + source + project dir, already known from `chain.Resolve`).

## 4. Data model (additive to `provenance.Report`)

```go
type OverviewEntry struct {
    Key      string `json:"key"`
    RawValue string `json:"raw_value"` // literal as written; ${...} unexpanded
}
type OverviewLayer struct {
    File    string          `json:"file"`              // abs path, or "(inline environment:)"
    Layer   string          `json:"layer"`             // "layer1" | "env_file" | "environment"
    Service string          `json:"service,omitempty"` // "" for chain; service name for runtime layers
    Entries []OverviewEntry `json:"entries"`           // declaration order
}
// Report gains:
//   Layers []OverviewLayer `json:"layers,omitempty"`
// Ordered: all Layer-1 chain files (in chain order), then per active service
// (sorted) its env_file layers (declared order) followed by its inline-environment
// layer. Markers are NOT stored — they are derived at render time from the walk.
```

All existing `Report` fields and the other modes are unchanged; `Layers` is only
populated for the `--overview` path (or always, cheaply — plan decides; if always,
it stays `omitempty`-clean for other modes' JSON only if empty, so populate lazily).

## 5. CLI surface (extends env-debug; daemon-free)

- `cenvkit env-debug --overview [--json]` — the two-section layered overview above.
  `--json` emits the `Layers` structure (markers are presentation-only; consumers
  recompute or ignore). `--overview` is a bool flag, mutually independent of the
  other mode flags (same pattern as `--chain`/`--files`).
- All other modes (`--chain`, `--files`, `--trace`, `--effective`, `--value`)
  unchanged.

## 6. Risks / plan-time verification (verify-before-claim)

- **Literal values + declaration order (MUST verify against compose-go v2.11.0).**
  The lens requires each file's entries in declaration order with `${...}` left
  literal. compose-go's `dotenv.Parse` returns a Go `map` (unordered) and may
  expand in-file references. The plan MUST confirm the actual behavior via `go doc`
  + a probe and, if `dotenv` does not yield ordered-literal output, use a thin
  ordered line reader for the raw entries (still parsing `KEY=VALUE`, quotes,
  `export`, comments consistently). Do NOT assert a specific `dotenv` API works
  without a probe.
- **Engine seam:** all the raw-entry gathering stays in `internal/engine`;
  `internal/provenance` stays pure Go (marker computation + render). CI seam check
  must still pass.
- **Determinism:** chain layers in chain order; services sorted; entries in
  declaration order → stable output for snapshot/JSON tests.
- **Plan-gated:** adds a `Report` field (sensitive seam) → a fresh plan-mode
  go-engineer produces a read-only implementation plan against this spec; architect
  approves before code (per CLAUDE.md risk gate). Then qa + code-review + lead git
  surgery.

## 7. Testing

- **provenance render (unit, qa):** hand-built `Report.Layers` fixtures →
  `RenderHuman` asserts `+`/`~ old → new`/`·` markers, the chain-vs-runtime section
  split, `inline environment:` as the last per-service layer, and the `⚠ gap` line;
  `--json` golden over the `Layers` schema. Pure Go, no docker.
- **engine (unit):** over `examples/monorepo` + scratch fixtures — assert `Layers`
  is populated in order with **literal** values (a `${VAR}`-containing entry stays
  literal) and the per-service env_file + inline layers are present.
- **acceptance:** new `env-debug --overview` assertion(s) on `examples/monorepo`
  (chain section shows `.env`/`.dev.env` layering; runtime shows `web` with
  `.web.env` + `.web.dev.env`; the `WEB_PORT` gap line appears). Count re-pinned in
  the plan with lead sign-off; header comments bumped in the same commit.
- All overview tests are daemon-free.

## 8a. Plan-review decisions (architect sign-off, 2026-06-17)

Resolutions on the plan-mode review (plan: `.claude/artifacts/2026-06-17-env-overview-plan.md`):

- **Probe verdict (§6) confirmed:** `dotenv` is unordered AND expands `${...}` →
  a thin ordered `KEY=VALUE` line reader in `internal/engine` is REQUIRED (not
  optional). It mirrors dotenv's surface tokenization for KEYS (skip blank/`#`;
  strip leading `export `; split first `=`; strip one matching quote pair; key
  charset `[A-Za-z0-9_.-]`) but takes the VALUE verbatim — no `${}` expansion, no
  escape processing.
- **D-A (populate gating): GATE behind `ProvInput.WantLayers`** (NOT always).
  Populate `Report.Layers` only on the `--overview` path. Keeps existing
  `--chain`/`--effective`/`--trace`/`--value` `--json` output byte-identical (no
  golden churn) and avoids bloating focused modes with the full layer dump.
- **D-B (unquoted trailing `#` trim): YES** — the line reader trims an unquoted
  trailing ` # comment` (mirrors dotenv `parser.go:157-159`) so a value isn't shown
  with comment text; quoted values and everything else stay verbatim.
- **Header source label: INCLUDE.** Add `ComposeEnvSource string` to
  `chain.Result` and tag the branch in `resolveComposeEnv` (shell `COMPOSE_ENV` /
  root `.env` / default `"dev"`); the header renders `COMPOSE_ENV = <value> (from
  <source>)`. (Touches `internal/chain` — go-engineer zone; additive, with a chain
  unit test.) This matches the sh kit's `dev (from .env)` the overview is restoring.
- **Project name in header:** use the project dir basename (cmd passes the dir);
  do NOT add an engine field for the resolved compose project name (YAGNI).
- **Count:** target **N=78** (75 + 3 overview acceptance assertions). qa reconciles
  the exact PASS-site tally and bumps the header comments in the same commit; lead
  signs off at integration.
- **Feasibility:** implementable as written with the §3 tightening above; no other
  spec corrections. Seam preserved; determinism met; gap lines are pure
  presentation over existing `Vars[].Gap`/`Effects`/`RuntimeDefs`.

## 8. Non-goals

- **Not a cross-environment diff** (dev vs prod side-by-side) — single resolved
  environment only. A cross-env diff could be a later mode.
- **Not resolved values** — that is `--effective` (kept as-is). `--overview` is the
  raw/layering lens.
- No new gap semantics — reuses v3's gap detection.
- No change to `--chain`/`--files`/`--trace`/`--effective`/`--value`.
