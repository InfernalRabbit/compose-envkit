package debug

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestValue_LastWins actually exercises precedence: BOTH files set SMOKE_VAL,
// and the LATER file (Layer-2 position) must win. RED on a first-wins mergeDotEnv.
func TestValue_LastWins(t *testing.T) {
	dir := t.TempDir()
	l1 := filepath.Join(dir, ".env")
	l2 := filepath.Join(dir, "svc.env")
	os.WriteFile(l1, []byte("SMOKE_VAL=l1-value\nOTHER=1\n"), 0o644)
	os.WriteFile(l2, []byte("SMOKE_VAL=l2-value\n"), 0o644)

	if got := Value([]string{l1, l2}, "SMOKE_VAL"); got != "l2-value" {
		t.Fatalf("Value=%q want l2-value (last-wins)", got)
	}
	if got := Value([]string{l1, l2}, "DEFINITELY_UNSET"); got != "" {
		t.Fatalf("unset var should be empty, got %q", got)
	}
}

// Option-B guard: v1 --value returns the RAW literal for a defaulted var, NOT
// the expanded default. RED on any impl that expands ${MISSING:-fallback}.
func TestValue_RawLiteral_NoDefaultExpansion(t *testing.T) {
	dir := t.TempDir()
	l1 := filepath.Join(dir, ".env")
	os.WriteFile(l1, []byte("WITH_DEFAULT=${MISSING:-fallback}\n"), 0o644)
	if got := Value([]string{l1}, "WITH_DEFAULT"); got != "${MISSING:-fallback}" {
		t.Fatalf("Value=%q want literal ${MISSING:-fallback} (Option B: no expansion)", got)
	}
}

// --value scope: Value reads Layer-1 only. A var set in BOTH layers must return
// the Layer-1 value when given the Layer-1 list, and the Layer-2 value when given
// the merged list (demonstrating the divergence the cmd wiring removes by feeding
// cr.Files to Value). RED if the wiring passed merged to Value.
func TestValue_ScopedToLayer1(t *testing.T) {
	dir := t.TempDir()
	l1 := filepath.Join(dir, ".env")           // Layer-1
	l2 := filepath.Join(dir, "web", "svc.env") // Layer-2
	os.MkdirAll(filepath.Dir(l2), 0o755)
	os.WriteFile(l1, []byte("PORT=l1\n"), 0o644)
	os.WriteFile(l2, []byte("PORT=l2\n"), 0o644)

	if got := Value([]string{l1}, "PORT"); got != "l1" {
		t.Fatalf("Layer-1 Value=%q want l1", got)
	}
	if got := Value([]string{l1, l2}, "PORT"); got != "l2" {
		t.Fatalf("merged Value=%q want l2 (proves the scope matters)", got)
	}
}

// --trace must still find a var present ONLY in Layer-2 (guards against an
// over-correction that scopes Trace to Layer-1).
func TestTrace_FindsLayer2OnlyVar(t *testing.T) {
	dir := t.TempDir()
	l1 := filepath.Join(dir, ".env")
	l2 := filepath.Join(dir, "web", "svc.env")
	os.MkdirAll(filepath.Dir(l2), 0o755)
	os.WriteFile(l1, []byte("ROOT=1\n"), 0o644)
	os.WriteFile(l2, []byte("SVC_PORT=18080\n"), 0o644)

	var buf bytes.Buffer
	Trace(&buf, []string{l1, l2}, "SVC_PORT") // merged list
	out := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("svc.env")) || !bytes.Contains(buf.Bytes(), []byte("18080")) {
		t.Fatalf("Trace did not find Layer-2-only SVC_PORT:\n%s", out)
	}
}
