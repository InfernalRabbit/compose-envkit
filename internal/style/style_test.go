package style_test

import (
	"os"
	"strings"
	"testing"

	"github.com/InfernalRabbit/compose-envkit/internal/provenance"
	"github.com/InfernalRabbit/compose-envkit/internal/style"
)

// ─── Resolve precedence tests ─────────────────────────────────────────────────
//
// All tests are headless / deterministic: we use explicit --color flag values
// for the forced paths and env-var injection for the env-based paths.
// os.Stdout is non-TTY in go test runners, so auto mode resolves to plain.

// TestResolve_NeverFlag: --color=never → plain regardless of TTY/env.
func TestResolve_NeverFlag(t *testing.T) {
	st := style.Resolve("never", os.Stdout)
	assertPlain(t, st, "never flag")
}

// TestResolve_AlwaysFlag: --color=always → ANSI even on a non-TTY.
func TestResolve_AlwaysFlag(t *testing.T) {
	st := style.Resolve("always", os.Stdout)
	assertANSI(t, st, "always flag (non-TTY)")
}

// TestResolve_Disabled: style.Disabled() returns a plain styler.
func TestResolve_Disabled(t *testing.T) {
	st := style.Disabled()
	assertPlain(t, st, "Disabled()")
}

// TestResolve_NO_COLOR: NO_COLOR set → plain, overrides TTY.
func TestResolve_NO_COLOR(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("CLICOLOR_FORCE", "")
	st := style.Resolve("auto", os.Stdout)
	assertPlain(t, st, "NO_COLOR=1 auto")
}

// TestResolve_CLICOLOR_FORCE: CLICOLOR_FORCE=1 → forces color on in auto mode.
func TestResolve_CLICOLOR_FORCE(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	st := style.Resolve("auto", os.Stdout)
	assertANSI(t, st, "CLICOLOR_FORCE=1 auto")
}

// TestResolve_NeverBeatsForce: --color=never overrides CLICOLOR_FORCE.
// §5 rule 1 (explicit flag) > rule 3 (env force).
func TestResolve_NeverBeatsForce(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")
	st := style.Resolve("never", os.Stdout)
	assertPlain(t, st, "never flag beats CLICOLOR_FORCE")
}

// TestResolve_NO_COLOR_beats_CLICOLOR_FORCE: when both NO_COLOR and
// CLICOLOR_FORCE are set, NO_COLOR wins (spec §5 rule 2 > rule 3).
// Pins the precedence against a future termenv-bump regression.
func TestResolve_NO_COLOR_beats_CLICOLOR_FORCE(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("CLICOLOR_FORCE", "1")
	st := style.Resolve("auto", os.Stdout)
	assertPlain(t, st, "NO_COLOR=1 beats CLICOLOR_FORCE=1")
}

// TestResolve_Auto_NonTTY: auto mode with non-TTY output → plain.
// go test stdout is non-TTY; we also clear forcing env vars.
func TestResolve_Auto_NonTTY(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "")
	st := style.Resolve("auto", os.Stdin) // os.Stdin is non-TTY in test runners
	assertPlain(t, st, "auto non-TTY (stdin)")
}

// ─── Disabled-styler semantic methods ────────────────────────────────────────

// TestDisabled_StringMethodsPassThrough: every string-in/string-out method on
// a Disabled styler returns its input byte-identical.
func TestDisabled_StringMethodsPassThrough(t *testing.T) {
	st := style.Disabled()
	const s = "test-value"
	for name, fn := range map[string]func(string) string{
		"Header":      st.Header,
		"Key":         st.Key,
		"Value":       st.Value,
		"Path":        st.Path,
		"Service":     st.Service,
		"Gap":         st.Gap,
		"GapName":     st.GapName,
		"Old":         st.Old,
		"Ok":          st.Ok,
		"Fail":        st.Fail,
		"Created":     st.Created,
		"Skipped":     st.Skipped,
		"ErrorMsg":    st.ErrorMsg,
		"SourceLabel": st.SourceLabel,
	} {
		if got := fn(s); got != s {
			t.Fatalf("Disabled.%s(%q) = %q, want byte-identical %q", name, s, got, s)
		}
	}
	// Marker / arrow methods (no arg) must return plain glyphs, no ANSI.
	for name, got := range map[string]string{
		"MarkerNew":      st.MarkerNew(),
		"MarkerOverride": st.MarkerOverride(),
		"MarkerRepeat":   st.MarkerRepeat(),
		"Arrow":          st.Arrow(),
	} {
		if strings.Contains(got, "\x1b") {
			t.Fatalf("Disabled.%s() = %q — must contain no ANSI escape", name, got)
		}
		if strings.TrimSpace(got) == "" {
			t.Fatalf("Disabled.%s() = %q — must return a non-empty glyph", name, got)
		}
	}
}

// TestEnabled_StringMethodsAddANSI: a forced-ANSI256 styler wraps strings
// with ANSI escape sequences. Checks a representative subset.
func TestEnabled_StringMethodsAddANSI(t *testing.T) {
	st := style.Resolve("always", os.Stdout)
	const s = "test-value"
	for name, fn := range map[string]func(string) string{
		"Header":  st.Header,
		"Key":     st.Key,
		"Path":    st.Path,
		"Service": st.Service,
		"Gap":     st.Gap,
		"GapName": st.GapName,
		"Ok":      st.Ok,
		"Fail":    st.Fail,
	} {
		got := fn(s)
		if !strings.Contains(got, "\x1b[") {
			t.Fatalf("enabled Styler.%s(%q) = %q — expected ANSI escape (\\x1b[)", name, s, got)
		}
	}
}

// TestEnabled_MarkerMethodsAddANSI: marker/arrow methods on a forced-ANSI256
// styler return glyphs wrapped with ANSI escapes.
func TestEnabled_MarkerMethodsAddANSI(t *testing.T) {
	st := style.Resolve("always", os.Stdout)
	for name, got := range map[string]string{
		"MarkerNew":      st.MarkerNew(),
		"MarkerOverride": st.MarkerOverride(),
		"MarkerRepeat":   st.MarkerRepeat(),
		"Arrow":          st.Arrow(),
	} {
		if !strings.Contains(got, "\x1b[") {
			t.Fatalf("enabled Styler.%s() = %q — expected ANSI escape (\\x1b[)", name, got)
		}
	}
}

// TestResolve_JSONDisabledPath: a Disabled styler (the --json path) is plain
// even when a forced-on human styler exists in the same process — no global
// state mutation (per-renderer profiles only; no lipgloss.SetColorProfile).
func TestResolve_JSONDisabledPath(t *testing.T) {
	_ = style.Resolve("always", os.Stdout) // human renderer (forced ANSI)
	jsonSt := style.Disabled()             // JSON renderer (always plain)

	got := jsonSt.Header("X")
	if strings.Contains(got, "\x1b") {
		t.Fatalf("Disabled (JSON path) produced ANSI after forced-on human styler in same process: %q", got)
	}
}

// TestDisabled_MarkerGlyphsNonEmpty: plain marker glyphs must be the expected
// ASCII characters (+ ~ · →) so --overview renders correctly in plain mode.
func TestDisabled_MarkerGlyphsNonEmpty(t *testing.T) {
	st := style.Disabled()
	if g := st.MarkerNew(); !strings.Contains(g, "+") {
		t.Fatalf("Disabled.MarkerNew() = %q, want '+' glyph", g)
	}
	if g := st.MarkerOverride(); !strings.Contains(g, "~") {
		t.Fatalf("Disabled.MarkerOverride() = %q, want '~' glyph", g)
	}
	if g := st.MarkerRepeat(); !strings.Contains(g, "·") {
		t.Fatalf("Disabled.MarkerRepeat() = %q, want '·' glyph", g)
	}
	if g := st.Arrow(); !strings.Contains(g, "→") {
		t.Fatalf("Disabled.Arrow() = %q, want '→' glyph", g)
	}
}

// TestResolve_InterfaceSatisfied: compile guard — Resolve and Disabled must
// return provenance.Styler. A compile error here means the API contract broke.
func TestResolve_InterfaceSatisfied(t *testing.T) {
	_ = style.Resolve("never", os.Stdout)
	_ = style.Disabled()
	t.Log("style.Resolve and style.Disabled satisfy provenance.Styler — compile guard OK")
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// assertPlain confirms no ANSI escapes from Header (the most prominently
// styled element; if anything produces ANSI it will be the header).
func assertPlain(t *testing.T, st provenance.Styler, label string) {
	t.Helper()
	const probe = "TEST_PLAIN"
	got := st.Header(probe)
	if strings.Contains(got, "\x1b") {
		t.Fatalf("[%s] Header(%q) = %q — expected plain (no \\x1b)", label, probe, got)
	}
	if !strings.Contains(got, probe) {
		t.Fatalf("[%s] Header(%q) = %q — probe text lost in plain styler", label, probe, got)
	}
}

// assertANSI confirms the styler wraps Header output with ANSI escape sequences.
func assertANSI(t *testing.T, st provenance.Styler, label string) {
	t.Helper()
	const probe = "TEST_ANSI"
	got := st.Header(probe)
	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("[%s] Header(%q) = %q — expected ANSI (\\x1b[) escape, got plain", label, probe, got)
	}
	if !strings.Contains(got, probe) {
		t.Fatalf("[%s] Header(%q) = %q — probe text lost in ANSI output", label, probe, got)
	}
}
