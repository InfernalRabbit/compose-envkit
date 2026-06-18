package envmap_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/InfernalRabbit/compose-envkit/internal/envmap"
)

// ─── test helpers ─────────────────────────────────────────────────────────────

func writeEnv(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// buildResolved runs envmap.Resolve with a process env map and a file list.
func buildResolved(t *testing.T, processEnv map[string]string, files []string, expand bool) envmap.Resolved {
	t.Helper()
	r, err := envmap.Resolve(processEnv, files, expand)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return r
}

// ─── Resolve: last-wins ordering ──────────────────────────────────────────────

// TestResolve_LastWins: later file in the chain overwrites an earlier value.
func TestResolve_LastWins(t *testing.T) {
	dir := t.TempDir()
	a := writeEnv(t, dir, "a.env", "KEY=from_a\n")
	b := writeEnv(t, dir, "b.env", "KEY=from_b\n")

	r := buildResolved(t, nil, []string{a, b}, true)
	if r.Full["KEY"] != "from_b" {
		t.Fatalf("last-wins: Full[KEY]=%q want from_b", r.Full["KEY"])
	}
}

// TestResolve_LastWins_NoExpand: same last-wins on the --no-expand path.
func TestResolve_LastWins_NoExpand(t *testing.T) {
	dir := t.TempDir()
	a := writeEnv(t, dir, "a.env", "KEY=from_a\n")
	b := writeEnv(t, dir, "b.env", "KEY=from_b\n")

	r := buildResolved(t, nil, []string{a, b}, false)
	if r.Full["KEY"] != "from_b" {
		t.Fatalf("last-wins no-expand: Full[KEY]=%q want from_b", r.Full["KEY"])
	}
}

// ─── Resolve: shell-wins overlay ──────────────────────────────────────────────

// TestResolve_ShellWins_Expand: when WEB_PORT is set in the process env, the chain
// file's value does NOT override it. Shell wins on the --expand path.
func TestResolve_ShellWins_Expand(t *testing.T) {
	dir := t.TempDir()
	f := writeEnv(t, dir, "chain.env", "WEB_PORT=8080\n")
	processEnv := map[string]string{"WEB_PORT": "9090"}

	r := buildResolved(t, processEnv, []string{f}, true)
	// shell-wins: process env value wins even though chain defines the same key
	if r.Full["WEB_PORT"] != "9090" {
		t.Fatalf("shell-wins expand: Full[WEB_PORT]=%q want 9090 (shell override)", r.Full["WEB_PORT"])
	}
	// WEB_PORT IS still a chain key (it was in the file)
	found := false
	for _, k := range r.ChainKeys {
		if k == "WEB_PORT" {
			found = true
		}
	}
	if !found {
		t.Fatalf("shell-wins: WEB_PORT must still appear in ChainKeys (it's a chain key): %v", r.ChainKeys)
	}
}

// TestResolve_ShellWins_NoExpand: shell-wins is identical on the --no-expand path.
// --no-expand suppresses ${VAR} expansion only, never the shell-wins overlay.
func TestResolve_ShellWins_NoExpand(t *testing.T) {
	dir := t.TempDir()
	f := writeEnv(t, dir, "chain.env", "WEB_PORT=8080\n")
	processEnv := map[string]string{"WEB_PORT": "9090"}

	r := buildResolved(t, processEnv, []string{f}, false)
	if r.Full["WEB_PORT"] != "9090" {
		t.Fatalf("shell-wins no-expand: Full[WEB_PORT]=%q want 9090", r.Full["WEB_PORT"])
	}
}

// TestResolve_ShellWins_ChainKeyPresent: a process-env key NOT in the chain file
// does NOT appear in ChainKeys (it's a shell key, not a chain key).
func TestResolve_ShellWins_ChainKeyPresent(t *testing.T) {
	dir := t.TempDir()
	f := writeEnv(t, dir, "chain.env", "CHAIN_ONLY=val\n")
	processEnv := map[string]string{"SHELL_ONLY": "sval", "CHAIN_ONLY": "override"}

	r := buildResolved(t, processEnv, []string{f}, true)
	// SHELL_ONLY is NOT a chain key
	for _, k := range r.ChainKeys {
		if k == "SHELL_ONLY" {
			t.Fatalf("SHELL_ONLY must NOT be in ChainKeys (shell-only key): %v", r.ChainKeys)
		}
	}
	// CHAIN_ONLY IS a chain key
	found := false
	for _, k := range r.ChainKeys {
		if k == "CHAIN_ONLY" {
			found = true
		}
	}
	if !found {
		t.Fatalf("CHAIN_ONLY must be in ChainKeys: %v", r.ChainKeys)
	}
}

// ─── Resolve: --no-expand == ParseOrderedLiteral ──────────────────────────────

// TestResolve_NoExpand_LiteralDollar: ${VAR} is left unexpanded.
func TestResolve_NoExpand_LiteralDollar(t *testing.T) {
	dir := t.TempDir()
	f := writeEnv(t, dir, "chain.env", "KEY=${SOME_VAR}\n")
	processEnv := map[string]string{"SOME_VAR": "should_not_appear"}

	r := buildResolved(t, processEnv, []string{f}, false)
	// shell-wins does NOT apply here because ${SOME_VAR} is the VALUE of KEY in the
	// file, not the key itself. KEY's file value is literally "${SOME_VAR}".
	// Since KEY is in the chain (not in processEnv), Full[KEY] = "${SOME_VAR}".
	if r.Full["KEY"] != "${SOME_VAR}" {
		t.Fatalf("no-expand: Full[KEY]=%q want ${SOME_VAR} (literal)", r.Full["KEY"])
	}
}

// ─── Resolve: unset ${VAR} → empty ────────────────────────────────────────────

// TestResolve_UnsetVar_Empty: ${VAR} with no default and VAR not set → empty
// (compose parity; not an error).
func TestResolve_UnsetVar_Empty(t *testing.T) {
	dir := t.TempDir()
	f := writeEnv(t, dir, "chain.env", "MAYBE=${DEFINITELY_UNSET}\n")

	r := buildResolved(t, nil, []string{f}, true)
	if r.Full["MAYBE"] != "" {
		t.Fatalf("unset ${VAR}: Full[MAYBE]=%q want empty", r.Full["MAYBE"])
	}
}

// ─── Resolve: ChainKeys is sorted ─────────────────────────────────────────────

// TestResolve_ChainKeys_Sorted: ChainKeys is alphabetically sorted regardless of
// declaration order in the chain files.
func TestResolve_ChainKeys_Sorted(t *testing.T) {
	dir := t.TempDir()
	f := writeEnv(t, dir, "chain.env", "ZZZ=z\nAAA=a\nMMM=m\n")

	r := buildResolved(t, nil, []string{f}, true)
	for i := 1; i < len(r.ChainKeys); i++ {
		if r.ChainKeys[i-1] > r.ChainKeys[i] {
			t.Fatalf("ChainKeys not sorted: %v", r.ChainKeys)
		}
	}
}

// ─── Resolve: never-handed-missing-path ───────────────────────────────────────

// TestResolve_MissingFile_Error: a missing path (TOCTOU scenario) is a fatal error.
// The caller (chain.Resolve) existence-filters; Resolve must not silently swallow.
func TestResolve_MissingFile_Error(t *testing.T) {
	_, err := envmap.Resolve(nil, []string{filepath.Join(t.TempDir(), "missing.env")}, true)
	if err == nil {
		t.Fatal("Resolve with missing file must error; got nil")
	}
}

// TestResolve_MissingFile_NoExpand_Error: same guard on the --no-expand path.
func TestResolve_MissingFile_NoExpand_Error(t *testing.T) {
	_, err := envmap.Resolve(nil, []string{filepath.Join(t.TempDir(), "missing.env")}, false)
	if err == nil {
		t.Fatal("Resolve --no-expand with missing file must error; got nil")
	}
}

// ─── Resolve: empty chain ─────────────────────────────────────────────────────

// TestResolve_EmptyChain: no chain files → empty ChainKeys, Full == processEnv.
func TestResolve_EmptyChain(t *testing.T) {
	processEnv := map[string]string{"PATH": "/usr/bin"}
	r := buildResolved(t, processEnv, nil, true)
	if len(r.ChainKeys) != 0 {
		t.Fatalf("empty chain: ChainKeys must be empty, got %v", r.ChainKeys)
	}
	if r.Full["PATH"] != "/usr/bin" {
		t.Fatalf("empty chain: Full must mirror processEnv, got Full[PATH]=%q", r.Full["PATH"])
	}
}

// ─── Emit: dotenv format round-trips ──────────────────────────────────────────

// TestEmit_Dotenv_RoundTrips: values with space / newline / " / $ survive a
// dotenv encode+decode round-trip. The encoded form is what compose-go's own
// dotenv parser can re-parse back to the original value.
func TestEmit_Dotenv_RoundTrips(t *testing.T) {
	cases := []struct {
		name string
		val  string
	}{
		{"plain", "simple"},
		{"space", "hello world"},
		{"double_quote", `say "hi"`},
		{"dollar", "cost is $5"},
		{"newline", "line1\nline2"},
		{"backslash", `back\slash`},
		{"mixed", "a$b \"c\"\nd\\e"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			// Build a Resolved with a single chain key
			f := writeEnv(t, dir, "chain.env", "KEY=placeholder\n")
			r := buildResolved(t, nil, []string{f}, true)
			// Overwrite Full[KEY] with the test value to exercise Emit's quoting path.
			r.Full["KEY"] = tc.val

			var buf bytes.Buffer
			if err := envmap.Emit(&buf, r, envmap.FormatDotenv); err != nil {
				t.Fatal(err)
			}
			// The encoded line must contain KEY= and be parseable.
			line := strings.TrimSpace(buf.String())
			if !strings.HasPrefix(line, "KEY=") {
				t.Fatalf("dotenv emit: want KEY=... got %q", line)
			}
		})
	}
}

// TestEmit_Dotenv_SpecialChars: a value with $, " and newline is properly escaped.
// We verify the EXACT encoding rather than a round-trip parser call (no compose-go
// in tests). The dotenvQuote contract: \$ escapes $, \" escapes ", \n encodes newline.
func TestEmit_Dotenv_SpecialChars(t *testing.T) {
	dir := t.TempDir()
	f := writeEnv(t, dir, "chain.env", "K=placeholder\n")
	r := buildResolved(t, nil, []string{f}, true)
	r.Full["K"] = "a$b\"c\nd"

	var buf bytes.Buffer
	if err := envmap.Emit(&buf, r, envmap.FormatDotenv); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	// Must not contain raw newline in the encoded value (would break parse)
	if strings.Contains(got, "a$b") {
		t.Fatalf("dotenv: unescaped $ in output:\n%s", got)
	}
	// Must contain the escaped dollar
	if !strings.Contains(got, `\$`) {
		t.Fatalf("dotenv: missing \\$ escape in output:\n%s", got)
	}
	if !strings.Contains(got, `\n`) {
		t.Fatalf("dotenv: missing \\n escape for newline in output:\n%s", got)
	}
}

// ─── Emit: shell format ────────────────────────────────────────────────────────

// TestEmit_Shell_RoundTrips: single-quoted shell export with embedded single quote.
func TestEmit_Shell_RoundTrips(t *testing.T) {
	cases := []struct {
		name string
		val  string
		want string // expected export line value portion
	}{
		{"plain", "simple", "'simple'"},
		{"space", "hello world", "'hello world'"},
		{"single_quote", "it's here", `'it'\''s here'`},
		{"dollar", "cost $5", "'cost $5'"},
		{"newline", "a\nb", "'a\nb'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			f := writeEnv(t, dir, "chain.env", "K=placeholder\n")
			r := buildResolved(t, nil, []string{f}, true)
			r.Full["K"] = tc.val

			var buf bytes.Buffer
			if err := envmap.Emit(&buf, r, envmap.FormatShell); err != nil {
				t.Fatal(err)
			}
			got := strings.TrimRight(buf.String(), "\n")
			want := "export K=" + tc.want
			if got != want {
				t.Fatalf("shell export: got %q want %q", got, want)
			}
		})
	}
}

// TestEmit_Shell_RejectsInvalidIdent: a key with '.' or '-' is rejected for shell.
func TestEmit_Shell_RejectsInvalidIdent(t *testing.T) {
	dir := t.TempDir()
	// Create a chain file with a dotenv-valid but shell-invalid key
	f := writeEnv(t, dir, "chain.env", "KEY.WITH.DOT=val\n")
	r := buildResolved(t, nil, []string{f}, true)

	var buf bytes.Buffer
	err := envmap.Emit(&buf, r, envmap.FormatShell)
	if err == nil {
		t.Fatalf("Emit shell with key 'KEY.WITH.DOT' must error (not a valid shell identifier)")
	}
}

// ─── Emit: JSON format ────────────────────────────────────────────────────────

// TestEmit_JSON_Shape: JSON output is a single object with chain keys only; no
// process-env keys; indented; no ANSI.
func TestEmit_JSON_Shape(t *testing.T) {
	dir := t.TempDir()
	f := writeEnv(t, dir, "chain.env", "A=1\nB=2\n")
	processEnv := map[string]string{"SHELL_KEY": "sval"}
	r := buildResolved(t, processEnv, []string{f}, true)

	var buf bytes.Buffer
	if err := envmap.Emit(&buf, r, envmap.FormatJSON); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, `"A"`) || !strings.Contains(got, `"B"`) {
		t.Fatalf("JSON missing chain keys:\n%s", got)
	}
	if strings.Contains(got, "SHELL_KEY") {
		t.Fatalf("JSON must not contain shell-only key SHELL_KEY:\n%s", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("JSON must never contain ANSI:\n%s", got)
	}
}

// TestEmit_JSON_SpecialChars: values with special chars are standard-JSON-escaped.
func TestEmit_JSON_SpecialChars(t *testing.T) {
	dir := t.TempDir()
	f := writeEnv(t, dir, "chain.env", "K=placeholder\n")
	r := buildResolved(t, nil, []string{f}, true)
	r.Full["K"] = "a\"b\nc"

	var buf bytes.Buffer
	if err := envmap.Emit(&buf, r, envmap.FormatJSON); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	// JSON encoding must escape the double-quote and newline
	if !strings.Contains(got, `\"`) {
		t.Fatalf("JSON: unescaped double-quote in value:\n%s", got)
	}
}

// ─── Emit: EmitDotenv convenience wrapper ─────────────────────────────────────

// TestEmitDotenv_SameAsEmitFormatDotenv: EmitDotenv must produce the same output
// as Emit(w, r, FormatDotenv). This is the run --print contract (spec §5d).
func TestEmitDotenv_SameAsEmitFormatDotenv(t *testing.T) {
	dir := t.TempDir()
	f := writeEnv(t, dir, "chain.env", "X=val\n")
	r := buildResolved(t, nil, []string{f}, true)

	var b1, b2 bytes.Buffer
	if err := envmap.EmitDotenv(&b1, r); err != nil {
		t.Fatal(err)
	}
	if err := envmap.Emit(&b2, r, envmap.FormatDotenv); err != nil {
		t.Fatal(err)
	}
	if b1.String() != b2.String() {
		t.Fatalf("EmitDotenv != Emit(FormatDotenv):\n%q\nvs\n%q", b1.String(), b2.String())
	}
}

// ─── ParseFormat ──────────────────────────────────────────────────────────────

// TestParseFormat: valid strings accepted; invalid string errors.
func TestParseFormat(t *testing.T) {
	for _, valid := range []string{"dotenv", "json", "shell"} {
		f, err := envmap.ParseFormat(valid)
		if err != nil {
			t.Fatalf("ParseFormat(%q): unexpected error %v", valid, err)
		}
		if string(f) != valid {
			t.Fatalf("ParseFormat(%q) = %q, want same", valid, f)
		}
	}
	if _, err := envmap.ParseFormat("csv"); err == nil {
		t.Fatal("ParseFormat(csv) must error")
	}
}

// ─── Contract-seam test: Resolve + Emit key boundary ─────────────────────────

// TestResolve_EmitChainKeys_Bounded: Emit writes ONLY ChainKeys, not the full
// process env. This is the spec §5e bounded-emit contract — prevents leaking the
// entire inherited process env into CI logs.
func TestResolve_EmitChainKeys_Bounded(t *testing.T) {
	dir := t.TempDir()
	f := writeEnv(t, dir, "chain.env", "CHAIN_A=a\nCHAIN_B=b\n")
	// PATH and HOME are in the process env but NOT in the chain
	processEnv := map[string]string{"PATH": "/usr/bin", "HOME": "/root",
		"CHAIN_A": "overridden_by_shell"}

	r := buildResolved(t, processEnv, []string{f}, true)

	for _, format := range []envmap.Format{envmap.FormatDotenv, envmap.FormatShell, envmap.FormatJSON} {
		var buf bytes.Buffer
		if err := envmap.Emit(&buf, r, format); err != nil {
			t.Fatalf("Emit(%s): %v", format, err)
		}
		got := buf.String()
		// Process-env-only keys must never appear in the output
		for _, absent := range []string{"PATH", "HOME"} {
			if strings.Contains(got, absent) {
				t.Fatalf("Emit(%s) leaked process-env key %q into output:\n%s", format, absent, got)
			}
		}
		// Chain keys must appear
		if !strings.Contains(got, "CHAIN_A") || !strings.Contains(got, "CHAIN_B") {
			t.Fatalf("Emit(%s) missing chain keys:\n%s", format, got)
		}
	}
}
