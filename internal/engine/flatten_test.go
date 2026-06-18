package engine_test

import (
	"path/filepath"
	"testing"

	"github.com/InfernalRabbit/compose-envkit/internal/engine"
)

// ─── Flatten ──────────────────────────────────────────────────────────────────

// TestFlatten_LastWins: later file in declaration order overwrites an earlier value.
func TestFlatten_LastWins(t *testing.T) {
	dir := t.TempDir()
	a := writeF(t, dir, "a.env", "KEY=from_a\n")
	b := writeF(t, dir, "b.env", "KEY=from_b\n")

	got, err := engine.Flatten(nil, []string{a, b}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got["KEY"] != "from_b" {
		t.Fatalf("last-wins: got %q want from_b", got["KEY"])
	}
}

// TestFlatten_LastWins_NoExpand: same last-wins rule on the --no-expand path.
func TestFlatten_LastWins_NoExpand(t *testing.T) {
	dir := t.TempDir()
	a := writeF(t, dir, "a.env", "KEY=from_a\n")
	b := writeF(t, dir, "b.env", "KEY=from_b\n")

	got, err := engine.Flatten(nil, []string{a, b}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got["KEY"] != "from_b" {
		t.Fatalf("no-expand last-wins: got %q want from_b", got["KEY"])
	}
}

// TestFlatten_BaseNotInResult: base feeds ${VAR} resolution but is NOT copied into
// the returned map. The caller owns the shell-wins overlay step.
func TestFlatten_BaseNotInResult(t *testing.T) {
	dir := t.TempDir()
	f := writeF(t, dir, "chain.env", "CHAIN_KEY=chain_val\n")
	base := map[string]string{"SHELL_KEY": "shell_val"}

	got, err := engine.Flatten(base, []string{f}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["SHELL_KEY"]; ok {
		t.Fatalf("base key SHELL_KEY must NOT appear in Flatten result (caller owns overlay): %v", got)
	}
	if got["CHAIN_KEY"] != "chain_val" {
		t.Fatalf("chain key CHAIN_KEY missing from result: %v", got)
	}
}

// TestFlatten_BaseUsedForExpansion: base feeds ${VAR} lookup during expansion but
// the expanded value comes from the file.
func TestFlatten_BaseUsedForExpansion(t *testing.T) {
	dir := t.TempDir()
	// chain file references ${BASE_VAR} which lives only in base, not the file.
	f := writeF(t, dir, "chain.env", "EXPANDED=${BASE_VAR:-default}\n")
	base := map[string]string{"BASE_VAR": "from_base"}

	got, err := engine.Flatten(base, []string{f}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got["EXPANDED"] != "from_base" {
		t.Fatalf("expansion used base: got %q want from_base", got["EXPANDED"])
	}
}

// TestFlatten_UnsetVarDefaultEmpty: ${VAR} with no default and VAR not in base
// resolves to empty (compose-go parity; not an error).
func TestFlatten_UnsetVarDefaultEmpty(t *testing.T) {
	dir := t.TempDir()
	f := writeF(t, dir, "chain.env", "MAYBE=${DEFINITELY_UNSET}\n")

	got, err := engine.Flatten(nil, []string{f}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got["MAYBE"] != "" {
		t.Fatalf("unset ${VAR} must resolve to empty, got %q", got["MAYBE"])
	}
}

// TestFlatten_NoExpand_LiteralDollar: --no-expand leaves ${...} unexpanded.
func TestFlatten_NoExpand_LiteralDollar(t *testing.T) {
	dir := t.TempDir()
	f := writeF(t, dir, "chain.env", "KEY=${SOME_VAR}\n")
	base := map[string]string{"SOME_VAR": "should_not_appear"}

	got, err := engine.Flatten(base, []string{f}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got["KEY"] != "${SOME_VAR}" {
		t.Fatalf("no-expand must leave ${SOME_VAR} literal, got %q", got["KEY"])
	}
}

// TestFlatten_EmptyFileList: no files → empty result (not an error).
func TestFlatten_EmptyFileList(t *testing.T) {
	got, err := engine.Flatten(nil, nil, true)
	if err != nil {
		t.Fatalf("Flatten with empty file list: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty result, got %v", got)
	}
}

// TestFlatten_MissingFile_Errors: missing file is a fatal error (callers feed only
// existence-filtered paths — MF2; a missing path reaching Flatten is a bug).
func TestFlatten_MissingFile_Errors(t *testing.T) {
	_, err := engine.Flatten(nil, []string{filepath.Join(t.TempDir(), "missing.env")}, true)
	if err == nil {
		t.Fatal("Flatten with missing file must error; got nil")
	}
}

// TestFlatten_MissingFile_NoExpand_Errors: same for the --no-expand path.
func TestFlatten_MissingFile_NoExpand_Errors(t *testing.T) {
	_, err := engine.Flatten(nil, []string{filepath.Join(t.TempDir(), "missing.env")}, false)
	if err == nil {
		t.Fatal("Flatten --no-expand with missing file must error; got nil")
	}
}

// ─── ParseOrderedLiteral ──────────────────────────────────────────────────────

// TestParseOrderedLiteral_Order: entries come out in declaration order.
func TestParseOrderedLiteral_Order(t *testing.T) {
	dir := t.TempDir()
	f := writeF(t, dir, "x.env", "A=1\nB=2\nC=3\n")

	entries, err := engine.ParseOrderedLiteral(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("want 3 entries, got %d: %v", len(entries), entries)
	}
	for i, want := range []string{"A", "B", "C"} {
		if entries[i].Key != want {
			t.Fatalf("entries[%d].Key = %q, want %q", i, entries[i].Key, want)
		}
	}
}

// TestParseOrderedLiteral_BlankAndComment: blank lines and #-comments are skipped.
func TestParseOrderedLiteral_BlankAndComment(t *testing.T) {
	dir := t.TempDir()
	f := writeF(t, dir, "x.env", "A=1\n\n# comment\nB=2\n")

	entries, err := engine.ParseOrderedLiteral(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries (blank+comment skipped), got %d: %v", len(entries), entries)
	}
}

// TestParseOrderedLiteral_DoubleQuoteStripped: double-quoted values have ONE quote
// pair stripped; interior is verbatim including special chars.
func TestParseOrderedLiteral_DoubleQuoteStripped(t *testing.T) {
	dir := t.TempDir()
	f := writeF(t, dir, "x.env", "KEY=\"hello world\"\n")

	entries, err := engine.ParseOrderedLiteral(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].RawValue != "hello world" {
		t.Fatalf("double-quoted: want RawValue=hello world, got %v", entries)
	}
}

// TestParseOrderedLiteral_SingleQuoteStripped: single-quoted values are stripped.
func TestParseOrderedLiteral_SingleQuoteStripped(t *testing.T) {
	dir := t.TempDir()
	f := writeF(t, dir, "x.env", "KEY='x y'\n")

	entries, err := engine.ParseOrderedLiteral(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].RawValue != "x y" {
		t.Fatalf("single-quoted: want RawValue=x y, got %v", entries)
	}
}

// TestParseOrderedLiteral_InlineComment: unquoted value trims a trailing " # comment".
func TestParseOrderedLiteral_InlineComment(t *testing.T) {
	dir := t.TempDir()
	f := writeF(t, dir, "x.env", "KEY=value # this is a comment\n")

	entries, err := engine.ParseOrderedLiteral(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].RawValue != "value" {
		t.Fatalf("inline comment: want RawValue=value, got %v", entries)
	}
}

// TestParseOrderedLiteral_DollarLiteral: ${VAR} is left unexpanded (verbatim).
func TestParseOrderedLiteral_DollarLiteral(t *testing.T) {
	dir := t.TempDir()
	f := writeF(t, dir, "x.env", "KEY=${SOME_VAR}\n")

	entries, err := engine.ParseOrderedLiteral(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].RawValue != "${SOME_VAR}" {
		t.Fatalf("dollar literal: want RawValue=${SOME_VAR}, got %v", entries)
	}
}

// TestParseOrderedLiteral_ExportPrefix: leading `export` keyword is stripped from keys.
func TestParseOrderedLiteral_ExportPrefix(t *testing.T) {
	dir := t.TempDir()
	f := writeF(t, dir, "x.env", "export KEY=val\n")

	entries, err := engine.ParseOrderedLiteral(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Key != "KEY" || entries[0].RawValue != "val" {
		t.Fatalf("export prefix: want KEY=val, got %v", entries)
	}
}

// TestParseOrderedLiteral_EmptyValue: KEY= yields an empty string RawValue.
func TestParseOrderedLiteral_EmptyValue(t *testing.T) {
	dir := t.TempDir()
	f := writeF(t, dir, "x.env", "KEY=\n")

	entries, err := engine.ParseOrderedLiteral(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].RawValue != "" {
		t.Fatalf("empty value: want RawValue='', got %v", entries)
	}
}

// TestParseOrderedLiteral_MissingFile_Errors: missing file returns an error.
func TestParseOrderedLiteral_MissingFile_Errors(t *testing.T) {
	_, err := engine.ParseOrderedLiteral(filepath.Join(t.TempDir(), "missing.env"))
	if err == nil {
		t.Fatal("ParseOrderedLiteral with missing file must error; got nil")
	}
}

// TestFlatten_EqualsParseOrderedLiteral_NoExpand: --no-expand path must produce
// the same key/value map as accumulating ParseOrderedLiteral entries last-wins.
// This is the contract-seam test: if Flatten(expand=false) ever diverges from
// ParseOrderedLiteral, something changed under one but not the other.
func TestFlatten_EqualsParseOrderedLiteral_NoExpand(t *testing.T) {
	dir := t.TempDir()
	a := writeF(t, dir, "a.env", "K=from_a\nX=xa\n")
	b := writeF(t, dir, "b.env", "K=from_b\nY=${UNEXPANDED}\n")

	// via Flatten(expand=false)
	flat, err := engine.Flatten(nil, []string{a, b}, false)
	if err != nil {
		t.Fatal(err)
	}

	// via manual ParseOrderedLiteral accumulation (last-wins)
	want := map[string]string{}
	for _, f := range []string{a, b} {
		entries, err := engine.ParseOrderedLiteral(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			want[e.Key] = e.RawValue
		}
	}

	for k, wv := range want {
		if flat[k] != wv {
			t.Fatalf("Flatten(expand=false)[%q] = %q, ParseOrderedLiteral gives %q", k, flat[k], wv)
		}
	}
	if len(flat) != len(want) {
		t.Fatalf("Flatten(expand=false) len=%d, ParseOrderedLiteral len=%d (maps: %v vs %v)", len(flat), len(want), flat, want)
	}
}
