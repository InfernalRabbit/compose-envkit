# Implementation plan — cenvkit `env-debug --overview`

Source-of-truth alongside `docs/superpowers/specs/2026-06-17-cenvkit-env-overview-design.md`.
Produced by plan-mode go-engineer (Opus), architect-approved 2026-06-17 with
decisions in spec §8a. **Feasibility: implementable** (with the §3 tightening).
compose-go v2.11.0 dotenv behavior probe-resolved below.

## 0. Probe finding (the §6 literal+ordered question) — RESOLVED

`dotenv` CANNOT yield ordered+literal entries — proven against v2.11.0:
1. **Unordered:** all entry points return `map[string]string` (`go doc …/dotenv`:
   `Parse`/`ReadFile`/`Read`/`ParseWithLookup`/`UnmarshalWithLookup`); `Parser`
   fills a `map` (`dotenv/format.go:36,42`). Probe: key order differs run-to-run.
2. **Expands `${...}`:** `dotenv/parser.go:160,197` → `expandVariables` →
   `dotenv/godotenv.go:170-182` = `template.Substitute`. Probe:
   `POSTGRES_USER=${DATABASE_POSTGRES_USER:-directus}` → `directus`; `${WEB_PORT:-0}`
   → `0`. The required literal cannot survive a dotenv call.

**Decision: a thin ordered `KEY=VALUE` line reader in `internal/engine`** returning
`[]provenance.OverviewEntry` (ordered, RAW). Mirror dotenv's KEY tokenization:
skip blank + `#`-comment lines; strip leading `export ` (`parser.go:101-105`,
`^export\s+`); split on first `=`; strip ONE matching surrounding quote pair; key
charset `[A-Za-z0-9_.-]` (`parser.go:122-127`). VALUE is verbatim (no `${}`
expansion, no escape processing) EXCEPT trim an unquoted trailing ` # comment`
(mirror `parser.go:157-159`) — decision D-B. `internal/chain/chain.go:62-87` has
~80% of this but returns a map, is in the wrong package (seam), and lacks
`export`-strip / inline-`#` handling → NEW function in engine; do not reuse/move
chain's.

## 1. Tasks (ordered)

- **T1 (model) `internal/provenance/model.go`:** add `OverviewEntry{Key,RawValue}`,
  `OverviewLayer{File,Layer,Service,Entries}`, `Report.Layers []OverviewLayer`
  (JSON tags per spec §4). No behavior change. Deps: none.
- **T2 (engine) `internal/engine/provenance.go`:** add
  `parseOrderedLiteral(path) ([]provenance.OverviewEntry, error)` (the line reader)
  + populate `rep.Layers` **only when `in.WantLayers`** (decision D-A). Deps: T1.
  Also add `WantLayers bool` to `engine.ProvInput`.
- **T2b (chain) `internal/chain/chain.go`:** add `ComposeEnvSource string` to
  `chain.Result` (values: `"shell"`/`".env"`/`"default"`); tag the branch in
  `resolveComposeEnv` (`chain.go:89-100`). Additive; chain unit test. (Header
  source label — decision.) Deps: none (parallel to T1/T2).
- **T3 (render) `internal/provenance/render.go`:** add `Overview bool` +
  `ComposeEnv`/`ComposeEnvSource`/`ProjectDir` (or a small header struct) to
  `HumanOpts`; an `overview` case in `RenderHuman` before `default`;
  `renderOverview` doing the marker-walk. Pure Go. Deps: T1.
- **T4 (cmd) `cmd/cenvkit/main.go`:** `--overview` bool flag; set
  `ProvInput.WantLayers = mOverview`; pass `Overview` + header inputs
  (`cr.ComposeEnv`, `cr.ComposeEnvSource`, `dir`) into `HumanOpts`. Deps: T2,T2b,T3.

## 2. Model delta (T1)

```go
type OverviewEntry struct {
    Key      string `json:"key"`
    RawValue string `json:"raw_value"` // literal; ${...} unexpanded
}
type OverviewLayer struct {
    File    string          `json:"file"`              // abs path, or "(inline environment:)"
    Layer   string          `json:"layer"`             // "layer1" | "env_file" | "environment"
    Service string          `json:"service,omitempty"` // "" chain; svc name for runtime
    Entries []OverviewEntry `json:"entries"`
}
// Report gains: Layers []OverviewLayer `json:"layers,omitempty"`
```
Populated only when `ProvInput.WantLayers` (D-A) → existing modes' `--json` unchanged.

## 3. Engine population (T2) — inside engine.Provenance, seam intact

- **Chain (layer1):** in the A-attribution loop (`provenance.go:124-144`, iterates
  `in.EnvFiles` in chain order, skips non-`layer1`) → for each existing layer1 file
  append `OverviewLayer{File, "layer1", "", parseOrderedLiteral(path)}`. Separate
  raw re-read from `parsed[i]` (which is expanded/unordered — §0). Missing files
  skipped (parity).
- **Per-service env_file:** in the C-loop (`provenance.go:225-265`, services in
  sorted `svcNames`, `svc.EnvFiles` declared order, `seenFile` dedup) → append
  `OverviewLayer{ef.Path, "env_file", name, parseOrderedLiteral(ef.Path)}` using the
  same dedup as `declaredFiles`.
- **Inline `environment:` (last per service):** from `svc.Environment`
  (`map[string]*string`, `provenance.go:252-259`; raw `*string`, NOT interpolated —
  C-load uses `WithoutEnvironmentResolution`, `provenance.go:201-208`). Key-sort for
  determinism; `RawValue = *vp` (nil→""). Emit only if non-empty.
  `File="(inline environment:)"`, `Layer="environment"`.
- **Determinism:** chain order; services sorted; env_file declared order; inline
  key-sorted.

## 4. Render (T3) — pure Go marker walk

`renderOverview(w, r, composeEnv, composeEnvSource, projectDir)`:
1. Header: `env overview — <basename(projectDir)> (mode: overview)`;
   `COMPOSE_ENV = <composeEnv> (from <source>)`; `Project dir = <projectDir>`.
2. **Section 1 — Interpolation chain (COMPOSE_ENV_FILES):** legend
   `+ new   ~ override   · repeat`; `acc := map[string]string{}`; walk
   `r.Layers` where `Layer=="layer1"` (chain order); per entry: not in acc → `+`;
   in acc & differs → `~ KEY = old → new`; equal → `·`.
3. **Section 2 — Runtime-only — service env_file: (NOT interpolated):** group
   `r.Layers` by `Service!=""` (engine emits env_file layers then the inline layer
   per service, in sorted service order — assert, don't re-sort); **fresh acc per
   service**; inline layer prints as `inline environment:`. After each service, a
   `⚠ gap:` line per var with `vt.Gap` and an `Effect.Service==svc`
   (fields from `vt.Effects[].Field`, fallback from `Effect.Resolved`) — pure
   presentation over existing gap data (`provenance.go:299-327`).
4. JSON: `RenderJSON` already serializes the whole `Report` → `Layers` rides along
   when populated.

## 5. cmd (T4) — main.go

`newEnvDebugCmd` (`main.go:247-302`): add `mOverview` to the flag group
(`:248-250`); `c.Flags().BoolVar(&mOverview,"overview",false,"per-file layering
overview (raw values, +/~/· markers)")`. Set `ProvInput.WantLayers = mOverview`.
In `HumanOpts` (`:286-289`): `Overview: mOverview` + header fields from `cr`
(`:260`) and `dir`. `--json` path (`:283-285`) unchanged.

## 6. Tests (qa) — target N=78 (75+3), lead sign-off

- **provenance render unit:** hand-built `Report.Layers` → `RenderHuman{Overview:true}`
  asserts `+`/`~ old → new`/`·`, two-section split, `inline environment:` last,
  `⚠ gap` line (Vars w/ Gap+Effects), header; `--json` golden over `Layers`.
- **engine unit:** over seeded examples/monorepo + scratch with a `${VAR}` value →
  `rep.Layers` populated ordered + LITERAL (the `${…:-…}` entries stay literal — the
  probe regression guard), chain before services, inline last. **Fixture caveat:**
  `examples/monorepo` `.env`/`.dev.env`/`web/.web.env` are gitignored — only
  `example.*` exist; seed via copy-to-tmp + `cenvkit init` (existing acceptance
  pattern) or scratch fixtures; never edit the committed fixture.
- **acceptance:** `--overview` assertions on seeded monorepo (chain `+ SITE_URL`
  then `~ SITE_URL`; runtime `web` `.web.env` layer; `WEB_PORT` gap line). Baseline
  75 (`test/cenvkit-acceptance_test.go:1,:25-26`) → +3 → **78**; header comments
  bumped same commit.

## 7. Risks / corrections (adversarial)

- **§3 wording corrected** (spec updated): engine reuses DISCOVERY only; entries =
  new ordered raw read (the existing `parsed[i]` is expanded/unordered).
- **Header source** not in `chain.Result` today → T2b adds it (decision: include).
- **D-A gate** avoids perturbing existing `--json` goldens.
- **WantLayers** must thread through `ProvInput` (cmd → engine) — small additive
  field.
- Seam preserved (line reader stdlib-only in engine); determinism met; gap reuse
  pure presentation.
- Non-goals (cross-env diff, resolved values) NOT planned.
