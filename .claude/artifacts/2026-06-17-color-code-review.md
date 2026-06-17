# cenvkit colored output (lipgloss) — code review

Reviewer: code-reviewer · Date: 2026-06-17 · Scope: uncommitted diff over
`cmd/ internal/ go.mod go.sum test/` on HEAD 4094631 + the new untracked
`internal/style/` package. Spec: `docs/superpowers/specs/2026-06-17-cenvkit-color-output-design.md`
(§4 palette, §5 precedence, §7a decisions). Plan: `.claude/artifacts/2026-06-17-color-output-plan.md`.

Verification (read-only): `go build ./...` OK · `go vet ./...` OK · `gofmt -l
cmd/ internal/ test/` CLEAN · `SMOKE_SKIP_DOCKER=1 go test ./...` all 8 pkgs GREEN
(incl. new `internal/style`) · `go mod tidy` is a no-op (go.mod/go.sum already
tidy) · binary 8.09 MiB (consistent with the +0.93 MiB the owner accepted).

## Verdict: READY TO INTEGRATE

No BLOCKERs, no SHOULD-FIX. Two minor NITs (one test-coverage gap, one cosmetic),
both optional. This is a clean, well-isolated, spec-faithful implementation: the
seam holds, no global state mutation, no AdaptiveColor, the nil-safe Styler keeps
existing tests byte-identical, and the precedence chain is correct (verified the
one delegated edge by probe).

---

## BLOCKER

None.

## SHOULD-FIX

None.

## Verified-correct (the load-bearing claims)

- **Profile control / no global mutation (focus 1):** `internal/style/style.go`
  uses ONLY `r.SetColorProfile(...)` per-renderer (`:108` Ascii, `:126` Ascii,
  `:128` ANSI256). Grep confirms NO `lipgloss.SetColorProfile` (global) anywhere
  (only in explanatory comments). `Disabled()` (`:106-110`) builds a SEPARATE
  renderer — never mutates the human one. `TestResolve_JSONDisabledPath`
  (`style_test.go:153`) guards this: a Disabled styler stays plain even after a
  forced-on human styler exists in the same process.
- **No AdaptiveColor (focus 2):** grep finds `AdaptiveColor`/`HasDarkBackground`
  only in a comment. Fixed ANSI-16 palette (`style.go:27-33`). No OSC query →
  no print-and-exit hang risk.
- **Precedence matches §5 EXACTLY (focus 3):** assembled across three layers,
  correctly ordered: rule 0 (`--json`→Disabled) in `cmd` (`main.go:319-323`,
  `RenderJSON` takes no Styler — structurally uncolorable); rule 1
  (`--color=never`→Ascii / `always`→ANSI256) explicit in `style.Resolve`
  (`:124-130`, above env); rules 2-4 (NO_COLOR > CLICOLOR_FORCE > TTY) delegated to
  termenv's auto-detect in the `auto` branch. `--color=always` overrides NO_COLOR
  (explicit flag never consults env — correct, §5 rule 1 > 2). **Probe-verified the
  one delegated edge the matrix doesn't cover:** with BOTH `NO_COLOR=1` and
  `CLICOLOR_FORCE=1`, termenv v0.16.0 resolves to Ascii (NO_COLOR wins) — matches
  §5 rule 2 > rule 3. No inversion. (See NIT-1: that both-set case is untested.)
- **Styler nil-safety (focus 4):** TWO nil-safe layers, neither dereferences nil.
  `cmd.currentStyler()` (`main.go:20-25`) returns `style.Disabled()` when the
  package `styler` is nil (e.g. a subcommand invoked without the root pre-run).
  `provenance.st()` (`render.go:305-310`) returns `plainStyler{}` when
  `HumanOpts.Style` is nil. `plainStyler` methods all return their arg / plain
  glyph. The env-debug human path always passes a non-nil `currentStyler()`.
  `TestRenderHuman_NilStyle_PlainOutput` + `_TraceUnchanged` confirm byte-identical
  plain with no Style.
- **Seam (focus 5):** `go list` import scan confirms `internal/style` is the ONLY
  lipgloss AND termenv importer; `internal/provenance` references them only in
  comments (no import); `internal/engine` remains the sole compose-go importer.
  The interface-in-provenance design (Styler defined in `render.go`, impl in
  `internal/style`, wired by `cmd`) keeps the styling lib out of the hot path.
- **go.mod/go.sum (focus 6):** lipgloss v1.1.0 is a DIRECT require; termenv
  v0.16.0 direct; transitives (go-isatty, go-colorful, go-runewidth, x/ansi,
  colorprofile, terminfo, uniseg, osc52) indirect; x/sys 0.5.0→0.30.0. `go mod
  tidy` no-op → tree is tidy. Binary 8.09 MiB, within the accepted delta.
- **Test quality (focus 7):** no-leak guards assert literal `\x1b` absence
  end-to-end through the binary (`TestColor_NoLeak_JSON` with `--json
  --color=always` is the key rule-0 guard; `_NeverFlag`/`_NOCOLOR`/`_DefaultNonTTY`).
  `TestColor_AlwaysFlag_OverviewHasANSI` is a genuine positive (RED if
  `--color=always` weren't wired). The `ansiStyler` fake matches the interface
  (compile-guard `var _ Styler = ansiStyler{}`, `render_test.go:644`) and the
  enabled render tests assert specific escape markers (not tautological).
  `RenderJSON`-no-ANSI guard is real. The 78 acceptance assertions + existing
  render tests are UNCHANGED (render_test.go diff is purely additive — zero
  deletions; acceptance diff is +5 guards after the existing block).

## NIT

### NIT-1 — precedence matrix doesn't test NO_COLOR + CLICOLOR_FORCE both set · qa-engineer
`internal/style/style_test.go`: `TestResolve_NO_COLOR` (`:37`) clears
CLICOLOR_FORCE and `TestResolve_CLICOLOR_FORCE` (`:45`) clears NO_COLOR — the
spec §5 rule 2 > rule 3 ordering (NO_COLOR wins when BOTH are set) is never
asserted. I probe-verified termenv v0.16.0 does the right thing (both-set →
Ascii), so this is a coverage gap, not a defect; the contract is delegated to
termenv and could silently change on a termenv bump. Optional: add one case
setting both and asserting plain. (Low priority — termenv owns this and the bump
checklist already covers dep changes.)

### NIT-2 — `--effective` value styling is cosmetic-only inside a tab-aligned line · go-engineer
`render.go:597-598` styles `e.Value` with `s.Value()` inside a `\t`-aligned
`%s=%s\t<- %s (%s)` line. `s.Value` is the normal (unstyled) palette entry
(`style.go:69` `value: ns()`), so today this wraps with no visible ANSI and the
tab alignment is unaffected. If a future palette gives `Value` a real color, the
embedded ANSI could perturb tab-stop alignment in `--effective`. No action now;
just a note that the `--effective` columnar layout + colored values may need a
width-aware align later. (Purely forward-looking; current output is correct.)

---

## Guard-validity check (standing duty)

- `TestColor_AlwaysFlag_OverviewHasANSI`: end-to-end through the binary; RED if
  `--color=always` → ANSI256 weren't wired into cmd. VALID.
- `TestColor_NoLeak_JSON` (`--json --color=always` → no `\x1b`): RED if the JSON
  path ever wired the human styler. VALID — guards rule 0.
- `TestRenderHuman_NilStyle_PlainOutput` / `_TraceUnchanged`: RED if `st()` weren't
  nil-safe (would panic) or if a default-plain path emitted ANSI. VALID — the
  zero-churn guarantee.
- `TestRenderHuman_ANSIStyle_OverviewMarkers`: asserts specific `\x1b[H`/`\x1b[N`/
  `\x1b[G` markers from the fake — RED if a styled element were left unstyled.
  Not tautological (the fake's markers are distinct per method).
- `TestEnabled_StringMethodsAddANSI` (real lipgloss forced ANSI256): RED if the
  palette didn't actually emit escapes. VALID — exercises the real renderer.
- `TestResolve_JSONDisabledPath`: RED if a global profile mutation leaked the
  forced-on human profile into the Disabled renderer. VALID — guards the
  no-global-mutation invariant.

## Seam / safety regression check

- Seam HOLDS: lipgloss + termenv ONLY in `internal/style`; provenance + engine
  + chain free of both; compose-go still ONLY in `internal/engine` (direct-import
  scan, not `-deps`).
- No behavior/exit-code/`--json`-schema change (purely presentational); the
  `--json` schema is untouched (no Styler reaches `RenderJSON`).
- No secrets to disk/log, no `chmod`/`sudo`. `--value` deliberately stays
  unstyled (`render.go:336`, script-friendly) — correct.
- `init` coloring is DEFERRED by design (spec §6) — not a finding.
