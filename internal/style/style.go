// Package style is the ONLY package that imports lipgloss. It provides a
// lipgloss-backed implementation of provenance.Styler plus the color-decision
// resolver. Keeping lipgloss isolated here preserves the seam rules: internal/
// provenance and cmd depend only on the provenance.Styler interface, never on a
// styling library, and internal/engine is untouched.
//
// Design notes (verified against lipgloss v1.1.0 / termenv v0.16.0):
//   - Per-renderer color profiles ONLY — we never call the global
//     lipgloss.SetColorProfile, so a colored human renderer and a plain (Ascii)
//     JSON renderer coexist safely in one process.
//   - FIXED ANSI palette, no lipgloss.AdaptiveColor: adaptive resolution issues a
//     blocking OSC background-color query to the terminal, a hang risk in a
//     print-and-exit CLI. The chosen ANSI-16 colors read on light and dark.
package style

import (
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/InfernalRabbit/compose-envkit/internal/provenance"
)

// Fixed ANSI-16 palette (legible on light + dark; no background query). Values are
// standard ANSI codes: 1 red, 2 green, 3 yellow, 5 magenta, 6 cyan.
const (
	colRed     = lipgloss.Color("1")
	colGreen   = lipgloss.Color("2")
	colYellow  = lipgloss.Color("3")
	colMagenta = lipgloss.Color("5")
	colCyan    = lipgloss.Color("6")
)

// Lipgloss implements provenance.Styler with a single renderer. Styles are built
// once at construction so each call is a cheap Render.
type Lipgloss struct {
	header      lipgloss.Style
	markerNew   lipgloss.Style
	markerOver  lipgloss.Style
	markerRep   lipgloss.Style
	arrow       lipgloss.Style
	key         lipgloss.Style
	value       lipgloss.Style
	old         lipgloss.Style
	path        lipgloss.Style
	service     lipgloss.Style
	sourceLabel lipgloss.Style
	gap         lipgloss.Style
	gapName     lipgloss.Style
	hint        lipgloss.Style
	ok          lipgloss.Style
	fail        lipgloss.Style
	created     lipgloss.Style
	skipped     lipgloss.Style
	errorMsg    lipgloss.Style
}

// newLipgloss builds the palette on a given renderer (its color profile decides
// whether styles actually emit ANSI — an Ascii-profile renderer renders plain).
func newLipgloss(r *lipgloss.Renderer) *Lipgloss {
	ns := r.NewStyle
	return &Lipgloss{
		header:      ns().Bold(true).Foreground(colCyan),
		markerNew:   ns().Foreground(colGreen),
		markerOver:  ns().Foreground(colYellow),
		markerRep:   ns().Faint(true),
		arrow:       ns().Faint(true),
		key:         ns().Bold(true),
		value:       ns(), // normal/readable
		old:         ns().Faint(true),
		path:        ns().Foreground(colCyan),
		service:     ns().Bold(true).Foreground(colMagenta),
		sourceLabel: ns().Faint(true),
		gap:         ns().Foreground(colRed),
		gapName:     ns().Bold(true).Foreground(colRed),
		hint:        ns().Faint(true),
		ok:          ns().Foreground(colGreen),
		fail:        ns().Foreground(colRed),
		created:     ns().Foreground(colGreen),
		skipped:     ns().Faint(true),
		errorMsg:    ns().Foreground(colRed),
	}
}

func (l *Lipgloss) Header(s string) string      { return l.header.Render(s) }
func (l *Lipgloss) MarkerNew() string           { return l.markerNew.Render("+") }
func (l *Lipgloss) MarkerOverride() string      { return l.markerOver.Render("~") }
func (l *Lipgloss) MarkerRepeat() string        { return l.markerRep.Render("·") }
func (l *Lipgloss) Arrow() string               { return l.arrow.Render("→") }
func (l *Lipgloss) Key(s string) string         { return l.key.Render(s) }
func (l *Lipgloss) Value(s string) string       { return l.value.Render(s) }
func (l *Lipgloss) Old(s string) string         { return l.old.Render(s) }
func (l *Lipgloss) Path(s string) string        { return l.path.Render(s) }
func (l *Lipgloss) Service(s string) string     { return l.service.Render(s) }
func (l *Lipgloss) SourceLabel(s string) string { return l.sourceLabel.Render(s) }
func (l *Lipgloss) Gap(s string) string         { return l.gap.Render(s) }
func (l *Lipgloss) GapName(s string) string     { return l.gapName.Render(s) }
func (l *Lipgloss) Hint(s string) string        { return l.hint.Render(s) }
func (l *Lipgloss) Ok(s string) string          { return l.ok.Render(s) }
func (l *Lipgloss) Fail(s string) string        { return l.fail.Render(s) }
func (l *Lipgloss) Created(s string) string     { return l.created.Render(s) }
func (l *Lipgloss) Skipped(s string) string     { return l.skipped.Render(s) }
func (l *Lipgloss) ErrorMsg(s string) string    { return l.errorMsg.Render(s) }

// Disabled returns a Styler that always renders plain (Ascii profile). Used for
// --json and any forced-plain path. It is a SEPARATE renderer from the human one,
// so forcing JSON plain never mutates the colored renderer.
func Disabled() provenance.Styler {
	r := lipgloss.NewRenderer(os.Stdout)
	r.SetColorProfile(termenv.Ascii)
	return newLipgloss(r)
}

// Resolve builds the Styler for the color decision (spec §5 precedence; the
// --json/forced-plain rule is handled by the caller using Disabled()):
//
//	flag=="never"  → Ascii (plain)
//	flag=="always" → ANSI256 (forced on, even when piped; NO_COLOR no longer applies
//	                 because --color is the explicit top-of-precedence override)
//	flag=="auto"   → NewRenderer(out) auto-detect: termenv handles NO_COLOR,
//	                 CLICOLOR_FORCE, TTY-gate, and CI-as-non-TTY for free.
//
// out is the stream color is gated on (os.Stdout for human output).
func Resolve(flag string, out *os.File) provenance.Styler {
	r := lipgloss.NewRenderer(out)
	switch flag {
	case "never":
		r.SetColorProfile(termenv.Ascii)
	case "always":
		r.SetColorProfile(termenv.ANSI256)
	default: // "auto" (and any unknown value) → let the renderer auto-detect
	}
	return newLipgloss(r)
}
