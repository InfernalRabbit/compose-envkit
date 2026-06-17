# cenvkit `env-debug --overview` — code review

Reviewer: code-reviewer · Date: 2026-06-17 · Scope: uncommitted working-tree diff
over `cmd/ internal/ test/` on top of HEAD d743aa1 (spec/plan committed at
4876629). Spec: `docs/superpowers/specs/2026-06-17-cenvkit-env-overview-design.md`
(§8a D-A/D-B + decisions). Plan: `.claude/artifacts/2026-06-17-env-overview-plan.md`.

Verification (read-only): `go build ./...` OK · `go vet ./...` OK · `gofmt -l
cmd/ internal/ test/` CLEAN · `SMOKE_SKIP_DOCKER=1 go test ./...` all GREEN · seam
holds (`parseOrderedLiteral` is stdlib-only `bufio`/`os`/`strings` in
`internal/engine`; `internal/provenance` + `internal/chain` stay compose-go-free;
only `internal/engine` imports compose-go).

## Verdict: CHANGES REQUIRED

No BLOCKERs. The core mechanism is correct: WantLayers gate is genuinely
code-gated, the literal line reader preserves `${...}` unexpanded (the probe
regression is guarded), the marker walk + per-service fresh accumulator + inline-
last + gap reuse are all sound, and the seam is preserved. The blocking items are
two SHOULD-FIX gaps in test fidelity (a plan-promised chain test is missing; an
acceptance fixture-seeding block is misleading/destructive) plus a minor line-
reader edge-case divergence. All qa-fixable; no prod redesign needed.

---

## BLOCKER

None.

Confirmed sound:
- **WantLayers gate (D-A, focus 2):** all three `rep.Layers` appends
  (`internal/engine/provenance.go:201`, `:313`, `:353`) are under `if in.WantLayers`
  (the inline one under `if in.WantLayers && len(svc.Environment) > 0`). Genuinely
  code-gated, not empty-by-accident. `TestProvenance_WantLayers_GateOff` +
  `TestRenderJSON_Layers_Schema` (asserts existing `files`/`chain_files`/`vars`/
  `in_chain` still present) guard it. Existing modes' `--json` unchanged.
- **Literal capture (focus 1, the probe regression):** `parseOrderedLiteral`
  (`provenance.go:141`) takes VALUE verbatim, no `${}` expansion.
  `TestProvenance_WantLayers_OrderedLiteral` asserts `POSTGRES_USER` stays
  `"${DATABASE_POSTGRES_USER:-directus}"` (RED on any dotenv-based impl, which
  would yield `"directus"`). Strong, docker-free guard.
- **Marker walk (focus 3):** chain accumulator single (`render.go:572`); per-service
  accumulator FRESH (`render.go:585` `svcAcc := map[string]string{}` inside the
  service loop); `~ old → new` correct (`classify` `render.go:638`); inline rendered
  last (engine emits it last per service, render iterates `r.Layers` in order);
  `·` only on equal values. No off-by-one / accumulator reuse.
- **Gap reuse (focus 4):** `renderServiceGaps` (`render.go:667`) is pure
  presentation over `Vars[].Gap` + `Effects[].Service/Field` — no new gap logic.
- **Chain-only `--overview`:** the chain-layer append (`provenance.go:199-203`) is
  inside the A loop, BEFORE the `len(configs)==0` early return (`:254`), so a
  no-compose-file project still shows the chain section. `TestOverview_ChainMarkers`
  exercises this.
- **Determinism (focus 6):** chain order; services sorted (svcNames); env_file
  declaration order (line reader); inline key-sorted (`provenance.go:233`).

## SHOULD-FIX

### SF-1 — plan/spec-promised chain unit test for `ComposeEnvSource` is MISSING · qa-engineer
Plan T2b and spec §8a both say the new `chain.Result.ComposeEnvSource` ships "with
a chain unit test." `internal/chain/chain_test.go` was **not modified** (empty diff
stat) and `grep -rn ComposeEnvSource --include=*_test.go` finds it only in
`internal/provenance/render_test.go:152,237` as a HARDCODED `".env"` fixture label
— that tests the *renderer*, not the chain's branch-tagging logic. The three
branches in `resolveComposeEnv` (`internal/chain/chain.go:70-84`: shell →
`"shell"`, root `.env` → `".env"`, else → `"default"`) have **zero direct
coverage**; a mislabel would not be caught. The acceptance overview tests also
don't assert the `(from <source>)` label value. Fix: add a table test in
`chain_test.go` over the three branches (shell-set, `.env`-only, neither).
(The branch logic itself reads correct under inspection — this is a coverage gap,
not a known bug.)

### SF-2 — `TestOverview_RuntimeWebLayer`/`_WEBPORTGap` seeding block is misleading AND clobbers the real fixture · qa-engineer
`test/cenvkit-acceptance_test.go:1234-1241` (and the WEBPORTGap twin) read
`web/example.web.env` and fall back to writing a minimal `WEB_PORT=18080` stub.
On disk: `examples/monorepo/web/.web.env` is **git-tracked** (657 bytes, present
after `stageMonorepo`'s `cp -R`), and `web/example.web.env` **does not exist**
(only root-level `example.*` exist; root dotfiles are gitignored, service ones are
tracked). So the block ALWAYS takes the `else` branch and **overwrites** the real
staged `.web.env` with the stub. Harmless to the current assertions (they check
presence + gap), but the comment ("seed … from example fixture … otherwise a
minimal") is false, and discarding the real fixture is fragile (a future
assertion on real `.web.env` keys would silently test the stub). Fix: delete the
seeding block — `.web.env` is already staged by `stageMonorepo` — or correct the
comment + guard on file existence so it does not overwrite.

### SF-3 — line reader: a value that is ONLY a trailing comment is not trimmed (dotenv divergence) · go-engineer
`parseOrderedLiteral` (`internal/engine/provenance.go:163-172`) does
`val := strings.TrimLeft(line[i+1:], " \t")` BEFORE the inline-`#` search
`strings.Index(val, " #")`. For `KEY= # comment` the leading space is trimmed
first, so `val` becomes `# comment` and the ` #` (space-hash) match fails →
RawValue renders as `# comment` (probe-confirmed). dotenv treats this as an empty
value (the comment is stripped). `--overview` would show `KEY = # comment`
literally. Low-frequency, but it is a wrong literal in the lens whose whole job is
fidelity. Fix: search for the inline comment on the pre-TrimLeft slice, or treat a
value that begins with `#` (after the `=`) as empty. (D-B says trim an unquoted
trailing ` # comment`; this case slips through the space-anchored match.)

## NIT

### N-1 — line reader: `export\t` (tab after export) not stripped · go-engineer
`provenance.go:158` `strings.TrimPrefix(line, "export ")` only strips a single
space; dotenv's `^export\s+` matches a tab too. `export<TAB>KEY=val` →
`validEnvKey("export\tKEY")` false → line dropped from the overview
(probe-confirmed). Rare. Optional: strip `export` then `TrimLeft(" \t")`.

### N-2 — gap line drops the resolved `:-default` the spec example shows · go-engineer
`render.go:687-689` emits `… → run falls back.` but the spec §2 example (lines
80-81) and plan §4 step 3 show `… → run falls back to :-0` (the `Effect.Resolved`
fallback). The data is already in `vt.Effects[].Resolved`. Minor: include it for
parity with the documented UX.

### N-3 — acceptance `overview-2b` is a near-tautological assertion · qa-engineer
`test/cenvkit-acceptance_test.go:1247` `strings.Contains(out, "web")` — "web"
appears in the project title, the `.web.env` path, and the service block
regardless. It is redundant with overview-2c (`.web.env`). Harmless; consider
asserting the `web:` service heading line specifically.

### N-4 — chain-only `--overview` prints an empty "Runtime-only" section header · go-engineer
When there is no compose file, `renderOverview` still prints the `Runtime-only —
service env_file:` header with no body (overviewServices is empty). Cosmetic;
optionally suppress the header when no service layers exist.

---

## Guard-validity check (standing duty)

- `TestProvenance_WantLayers_OrderedLiteral` (literal `${...}`): RED on any
  dotenv-based impl (would expand to `directus`/`18080`). VALID, docker-free.
- `TestProvenance_WantLayers_GateOff` (Layers empty when not requested): RED on an
  always-populate impl. VALID.
- `TestProvenance_WantLayers_DeclarationOrder` (Z_FIRST before A_SECOND): RED on a
  map-based (sorted/unordered) impl. VALID — guards the ordered-read requirement.
- `TestRenderHuman_Overview_*` (markers / two-section / inline-last / gap / header):
  exercise all three marker paths over a hand-built fixture; the inline-last
  assertion checks output index order. SOLID.
- `TestRenderJSON_Layers_Schema`: asserts the literal `${WEB_PORT:-0}` survives into
  JSON AND existing fields remain. Good regression backstop for D-A.
- `TestRenderFiles_FullyOverriddenEnvFileStillListed` (the N-3 fix from v3 review):
  present at `render_test.go` — confirms the `ServiceEnv.EnvFiles` field now drives
  `--files` so a fully-inline-overridden env_file still lists. Good close-out.

Gaps: SF-1 (chain branch tagging untested) and the SF-2 seeding masking real
fixture content are the coverage weaknesses; the rest of the suite genuinely
guards the contract.

## Seam / safety regression check

- Seam HOLDS: line reader stdlib-only in `internal/engine`; `internal/provenance`
  + `internal/chain` compose-go-free; only `internal/engine` imports compose-go
  (direct-import check, not `-deps`).
- No new shell-out, no secrets to disk/log, no `chmod`/`sudo` in the diff. The
  raw line reader reads files already in the chain/service set; `--overview` shows
  raw values, which is the intended lens (no NEW secret exposure beyond what
  `--effective`/`--chain` already render).
- `--overview` is additive; no change to `--chain`/`--files`/`--trace`/
  `--effective`/`--value` behavior or JSON (D-A verified).
