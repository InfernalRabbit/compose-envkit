# Implementation plan — cenvkit colored/formatted output (lipgloss)

Source-of-truth alongside `docs/superpowers/specs/2026-06-17-cenvkit-color-output-design.md`.
Produced by plan-mode go-engineer (Opus), architect-approved 2026-06-17 (spec §7a).
**Feasibility: GREEN, additive, no engine-contract change.**

## 0. Probe findings (verified, not guessed)

Pinned by `go get`: lipgloss v1.1.0, termenv v0.16.0, go-isatty v0.0.20,
go-colorful v1.2.0 (+ x/ansi, colorprofile, go-runewidth; x/sys 0.5.0→0.30.0).

- **Per-renderer profile, no global mutation.** `lipgloss.NewRenderer(out)`
  (`renderer.go:44`); `Renderer.SetColorProfile(p)` sets a per-renderer explicit
  profile bypassing auto-detect (`renderer.go:102-108`). AUTO via
  `ColorProfile()`→termenv `EnvColorProfile` (`renderer.go:66-79`): NO_COLOR→Ascii,
  CLICOLOR_FORCE→force, else TTY-gate; `isTTY` false when `CI` set
  (`termenv.go:28-40`). Profiles: `TrueColor=0, ANSI256, ANSI, Ascii`.
- **Force plain** (disabled / `--json`): `SetColorProfile(termenv.Ascii)`.
- **Force color when piped** (`--color=always`): `SetColorProfile(termenv.ANSI256)`
  (NOT `WithTTY` — NO_COLOR still beats WithTTY).
- **AdaptiveColor REJECTED (correction a):** `HasDarkBackground()` issues a
  blocking OSC query (`output.go:156-178`) — hang risk in print-and-exit. Use a
  fixed ANSI palette legible on both backgrounds; no query.
- Style API: `r.NewStyle().Foreground(lipgloss.Color("…")).Bold(true).Faint(true).Render(s)`.
  Compile-probed clean.
- Seam: lipgloss ≠ compose-go; CI seam check greps `compose-spec/compose-go`
  (`.github/workflows/ci.yml:37-55`); `internal/engine` untouched. No violation.

## 1. internal/style design — interface-in-provenance

- **`internal/provenance` defines a minimal `Styler` interface** (semantic methods
  per palette §4): `Header(s)`, `MarkerNew()/MarkerOverride()/MarkerRepeat()`,
  `Key/Value/Path/Service/SourceLabel/Gap/GapName(s)`, `Ok/Fail/Created/Skipped/
  ErrorMsg(s)`, `Arrow()`, `Old(s)`. `HumanOpts` gains `Style Styler`.
- **Nil-safe**: local `st(o.Style)` helper returns the field or a `plainStyler{}`
  (each method returns arg unchanged). Existing render tests build `HumanOpts`
  with NO `Style` → byte-identical plain → ZERO churn (confirmed render_test.go
  omits Style throughout).
- **`internal/style`** (ONLY lipgloss importer): `type Lipgloss struct{ r *lipgloss.Renderer; … }`
  implementing the interface; `Resolve(flag string, out *os.File) Styler` (§5
  precedence); `Disabled() Styler` (Ascii) for `--json`/forced-plain. Fixed palette
  (no AdaptiveColor).

## 2. Ordered tasks

- **T2 (internal/style)** — new pkg: `Lipgloss` impl, `Resolve`, `Disabled`, fixed
  palette §4. Deps: none. (Do before T1 tidy so the import exists.)
- **T3 (internal/provenance/render.go)** — define `Styler` interface + `plainStyler`
  + `st()`; add `Style` to `HumanOpts`; thread the styler through
  renderOverview/renderEffective/renderTrace/renderFiles/Chain default + gap lines
  + headers + legend + markers (`render.go:37-292`). Interface is local → no new
  import in the hot test path.
- **T4 (cmd/cenvkit/main.go)** — persistent `--color=auto|always|never` flag on
  root; resolve ONE styler in root `PersistentPreRunE` via `style.Resolve(flag,
  os.Stdout)`; thread into: env-files print (`:121`), validate ok/fail
  (`:198-227`), init created/skipped, error printing (`:321`, stderr, red), version
  (plain). env-debug: `HumanOpts.Style` = resolved styler, EXCEPT the `--json`
  branch (`:284-285`) → `style.Disabled()`. Read the persistent flag via
  `cmd.Root().PersistentFlags()` (cobra gotcha). Deps: T2, T3.
- **T1 (go.mod)** — `go mod tidy` (lipgloss becomes direct once T2 imports it);
  verify `go build`/`vet`/seam clean; measure binary-size delta. Files: go.mod,
  go.sum. Do LAST.

## 3. Control wiring (§5 precedence)

0. `--json` path → `style.Disabled()` (Ascii), separate renderer, never the human
   one. Top rule, overrides `--color=always`.
1. `flag=="never"`→Disabled; `flag=="always"`→`NewRenderer(out)`+`SetColorProfile(ANSI256)`;
   `flag=="auto"`→fall through.
2-4. auto: `NewRenderer(out)`, let `ColorProfile()` auto-resolve (NO_COLOR /
   CLICOLOR_FORCE / TTY / CI all handled by termenv for free).

## 4. Test plan (qa)

- **internal/style unit (NEW):** `Resolve` precedence matrix (`--color` × NO_COLOR
  × CLICOLOR_FORCE × tty-true/false via `termenv.WithTTY`/explicit profile,
  headless-deterministic); `Disabled` styler `Render==input` (byte-identical
  plain); enabled (forced ANSI256) wraps with `\x1b[`.
- **provenance render (qa zone):** existing tests pass UNCHANGED (nil styler ⇒
  plain). ADD: one forced-enabled styler asserting ANSI around marker+header+gap;
  one asserting `RenderJSON` has NO `\x1b`.
- **acceptance (78):** unchanged — non-TTY/CI ⇒ plain (no-leak guard). Optional +1:
  `--color=always` then assert an escape in `--overview`.
- Count delta: +0 acceptance (maybe +1 optional); new internal/style unit file;
  +~2-3 render tests. No existing test edited.

## 5. Risks

- Dep weight ~9 modules (owner relaxed thin for this feature); measure binary size
  in T1; no compose-go leak (seam verified).
- AdaptiveColor blocking-query → avoided (fixed palette).
- Per-renderer profiles only → JSON-plain vs human-color within one process safe;
  never call global `lipgloss.SetColorProfile`.
- Windows/CI handled by termenv; CI auto-plain.
- Not an engine-contract change.

## Note
The probe `go get` left go.mod/go.sum modified in the working tree (lipgloss
`// indirect` + x/sys bump) — uncommitted. T2's import makes lipgloss direct; T1's
`go mod tidy` finalizes. Impl builds on this state.
