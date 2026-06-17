---
name: lipgloss-termenv-color-control
description: Verified lipgloss v1.1.0 + termenv v0.16.0 profile-control mechanism for TTY-aware color (per-renderer, no global mutation) — the cenvkit color feature probe
metadata:
  type: reference
---

Probed 2026-06-17 for the cenvkit color-output feature. lipgloss is a string-styling
lib (renders to strings, pipe-safe) — NOT interactive. Resolved versions when
`go get github.com/charmbracelet/lipgloss`: **lipgloss v1.1.0**, termenv v0.16.0,
go-isatty v0.0.20, go-colorful v1.2.0 (+ x/ansi, x/term, colorprofile, etc).

Verified API (source under GOMODCACHE, not guessed):

- `lipgloss.NewRenderer(w io.Writer, ...termenv.OutputOption) *Renderer` — the
  per-call renderer. AUTO color path: `Renderer.ColorProfile()` lazily calls
  `output.EnvColorProfile()` (renderer.go:66-79), which honors NO_COLOR (→Ascii),
  CLICOLOR/CLICOLOR_FORCE, and TTY-ness of the writer (`isTTY()` does
  `isatty.IsTerminal(f.Fd())` on a `*os.File`; returns false if `CI` env set,
  termenv.go:28-40). So a plain non-TTY writer auto-degrades to Ascii.
- FORCE PLAIN (disabled / `--json` path): `r.SetColorProfile(termenv.Ascii)` —
  sets `explicitColorProfile=true` so auto-detection is bypassed (renderer.go:102).
  Per-renderer; NO global state mutated. Profile constants: TrueColor=0, ANSI256,
  ANSI, **Ascii** (termenv profile.go:16-23). `lipgloss.SetColorProfile()` (global,
  on DefaultRenderer) EXISTS but DON'T use it — would cross the JSON/human paths.
- FORCE COLOR when piped (`--color=always`): either
  `lipgloss.NewRenderer(out, termenv.WithTTY(true))` (assumeTTY=true), or
  `r.SetColorProfile(termenv.ANSI256)`. WithTTY only flips the TTY assumption;
  NO_COLOR would still win via EnvColorProfile, so for a hard force prefer an
  explicit non-Ascii SetColorProfile.
- Style: `r.NewStyle().Foreground(c TerminalColor).Bold(true).Faint(true).Render(s)`
  — confirmed. `lipgloss.Color("2")`, `lipgloss.AdaptiveColor{Light,Dark}` both real
  (AdaptiveColor resolves via `r.HasDarkBackground()`, color.go:100/158).
- GOTCHA: `HasDarkBackground()` lazily issues a **blocking OSC background-color
  query** to the terminal (output.go:156-178 → BackgroundColor). Call
  `r.SetHasDarkBackground(true)` explicitly (sets explicit flag) to avoid the query
  / a hang, OR avoid AdaptiveColor and pick ANSI-16 colors that read on both bgs.
- Snippet `go vet` clean (exit 0) with NewRenderer + SetColorProfile + WithTTY +
  AdaptiveColor + Foreground/Bold/Render — the recommended construction compiles.

Seam: lipgloss is unrelated to compose-go; CI seam check only greps
`compose-spec/compose-go` outside internal/engine (.github/workflows/ci.yml:37-55),
so adding lipgloss to internal/style + cmd is NO seam violation. Only
internal/engine imports compose-go today.

NOTE: after the probe `go get`, lipgloss sits as `// indirect` in go.mod (no .go
imports it yet); `go mod tidy` would strip it until internal/style imports it.
See [[compose-go-api-facts]] for the parallel "verify via go doc when context7 down" pattern.

IMPLEMENTATION OUTCOME (task #7, built 2026-06-17):
- The `Styler` INTERFACE lives in `internal/provenance` (render.go), NOT in
  internal/style. internal/style is the ONLY lipgloss importer (concrete
  `Lipgloss` impl + `Resolve(flag, out)` + `Disabled()`); cmd wires it. A nil
  `HumanOpts.Style` falls back to a `plainStyler` (each method returns its arg) →
  existing render tests stay byte-identical, ZERO churn. provenance + engine stay
  lipgloss-free.
- Used a FIXED ANSI-16 palette (Color "1".."6"), NOT AdaptiveColor — avoids the
  blocking OSC query. Per-renderer SetColorProfile only; never the global.
- Precedence wired: `--json` always plain (RenderJSON emits no styling — beats
  --color=always); --color=never→Ascii; --color=always→ANSI256; auto→NewRenderer
  lets termenv honor NO_COLOR/CLICOLOR_FORCE/TTY/CI. Human gates on os.Stdout;
  ERROR output gates on os.Stderr (separate styler in main, --color read via an
  args scan since PersistentPreRunE may not run on a parse error).
- Binary cost: +~0.93 MiB (+13%): 7.16→8.09 MiB. Owner relaxed thin-dep for this
  feature only.
- `init` created/skipped coloring: bootstrap.Init prints NOTHING today, so there's
  no existing output to color — adding it is a behavior change (consulted lead;
  separable follow-up, not pure styling). Related: [[cobra-persistent-flag-read-from-root]].
- LATENT (review NIT-2, no action taken): `Value` is intentionally the no-op
  palette entry (`value: ns()`) so `--effective`'s `\t`-aligned KEY=VALUE lines
  carry zero ANSI and the tab-stops stay aligned. IF a future palette gives `Value`
  a real foreground, embedded ANSI will perturb the columnar `--effective` layout —
  fix `renderEffective` alignment in the SAME change (strip-ANSI-before-pad or
  lipgloss width-aware columns). Don't color Value without that.
