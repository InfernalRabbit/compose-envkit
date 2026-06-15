package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

func wf(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInit_NoClobberSecrets(t *testing.T) {
	dir := t.TempDir()
	wf(t, filepath.Join(dir, "example.secrets.env"), "TOKEN=changeme\n")
	wf(t, filepath.Join(dir, ".secrets.env"), "TOKEN=REAL-PRODUCTION-SECRET\n") // populated
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dir, ".secrets.env")); got != "TOKEN=REAL-PRODUCTION-SECRET\n" {
		t.Fatalf("init clobbered an existing secret file: %q", got)
	}
}

func TestInit_SeedsFanOutIdempotent(t *testing.T) {
	dir := t.TempDir()
	wf(t, filepath.Join(dir, "example.env"), "ROOT=1\n")
	wf(t, filepath.Join(dir, "example.dev.env"), "TIER=dev\n")
	wf(t, filepath.Join(dir, "sub", "example.env"), "SUB=1\n")

	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".env", ".dev.env", filepath.Join("sub", ".env")} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Fatalf("expected seeded %s: %v", p, err)
		}
	}
	// Idempotent: second run must not error and must not change seeded files.
	before := read(t, filepath.Join(dir, ".env"))
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dir, ".env")) != before {
		t.Fatal("re-run changed an already-seeded file")
	}
}
