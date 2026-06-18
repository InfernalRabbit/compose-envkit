package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// version subcommand output via the cobra OutOrStdout() wiring (spec §5).
func TestVersionSubcommand(t *testing.T) {
	root := newRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := bytes.TrimSpace(buf.Bytes()); string(got) != version {
		t.Fatalf("version output = %q, want %q", got, version)
	}
}

// --project-dir is registered AND its set-branch flows through resolveProjectDir
// (the acceptance suite drives scope via cwd and never sets this flag, so the
// flag-set branch is otherwise dead).
func TestProjectDirFlagWiring(t *testing.T) {
	root := newRootCmd()
	if root.PersistentFlags().Lookup("project-dir") == nil {
		t.Fatal("--project-dir persistent flag not registered")
	}
	tmp := t.TempDir()
	if err := root.PersistentFlags().Set("project-dir", tmp); err != nil {
		t.Fatal(err)
	}
	got, err := resolveProjectDir(root)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.Abs(tmp)
	if got != want {
		t.Fatalf("resolveProjectDir=%q want %q (flag-set branch)", got, want)
	}
}

// ─── gap-report command tests ──────────────────────────────────────────────────

// writeGapFixture creates a project dir whose web service references ${WEB_PORT}
// (in ports) but defines WEB_PORT only in a service env_file: — a #3435 gap.
// If inChain is true, WEB_PORT is also put in the Layer-1 .env (so no gap).
func writeGapFixture(t *testing.T, inChain bool) string {
	t.Helper()
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "compose.yaml"),
		"services:\n  web:\n    image: nginx\n    env_file:\n      - web.env\n    ports:\n      - \"${WEB_PORT:-0}:80\"\n")
	mustWriteFile(t, filepath.Join(dir, "web.env"), "WEB_PORT=8080\n")
	env := ""
	if inChain {
		env = "WEB_PORT=8080\n"
	}
	mustWriteFile(t, filepath.Join(dir, ".env"), env)
	return dir
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// clearGapEnv unsets env vars that could taint a hermetic fixture run.
// t.Setenv only sets (never unsets), so we explicitly unset with a cleanup
// to restore the original value so we don't poison parallel tests.
func clearGapEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"COMPOSE_FILE", "COMPOSE_ENV_FILES", "COMPOSE_ENV", "WEB_PORT"} {
		prev, hadPrev := os.LookupEnv(k)
		os.Unsetenv(k) //nolint:errcheck
		t.Cleanup(func() {
			if hadPrev {
				os.Setenv(k, prev) //nolint:errcheck
			} else {
				os.Unsetenv(k) //nolint:errcheck
			}
		})
	}
}

// runGapReport executes `gap-report --project-dir <dir> [extra...]` against the
// root command and returns (stdout+stderr combined, error). error is nil on a clean exit.
func runGapReport(t *testing.T, dir string, extra ...string) (string, error) {
	t.Helper()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"gap-report", "--project-dir", dir}, extra...))
	err := root.Execute()
	return out.String(), err
}

func TestGapReportExitsOneOnGap(t *testing.T) {
	clearGapEnv(t)
	dir := writeGapFixture(t, false) // WEB_PORT only in web.env (service env_file:)
	out, err := runGapReport(t, dir)
	var ee *exitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exitError code 1, got err=%v out=%s", err, out)
	}
	if !bytes.Contains([]byte(out), []byte("WEB_PORT")) {
		t.Fatalf("gap output missing WEB_PORT:\n%s", out)
	}
}

func TestGapReportExitsZeroWhenClean(t *testing.T) {
	clearGapEnv(t)
	dir := writeGapFixture(t, true) // WEB_PORT now in the Layer-1 chain
	out, err := runGapReport(t, dir)
	if err != nil {
		t.Fatalf("want clean exit (nil), got %v out=%s", err, out)
	}
}

func TestGapReportExitsTwoWithoutComposeFile(t *testing.T) {
	clearGapEnv(t)
	dir := t.TempDir() // no compose.yaml
	mustWriteFile(t, filepath.Join(dir, ".env"), "FOO=bar\n")
	_, err := runGapReport(t, dir)
	var ee *exitError
	if !errors.As(err, &ee) || ee.ExitCode() != 2 {
		t.Fatalf("want exitError code 2, got %v", err)
	}
}

func TestGapReportJSON(t *testing.T) {
	clearGapEnv(t)
	dir := writeGapFixture(t, false)
	out, err := runGapReport(t, dir, "--json")
	var ee *exitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got %v", err)
	}
	for _, want := range []string{`"count": 1`, `"var": "WEB_PORT"`, `"field": "ports[0]"`} {
		if !bytes.Contains([]byte(out), []byte(want)) {
			t.Fatalf("json missing %q:\n%s", want, out)
		}
	}
}

// TestGapReportJSON_NoANSI: --json output must never contain ANSI escapes regardless of --color.
func TestGapReportJSON_NoANSI(t *testing.T) {
	clearGapEnv(t)
	dir := writeGapFixture(t, false)
	out, _ := runGapReport(t, dir, "--json", "--color=always")
	if bytes.Contains([]byte(out), []byte("\x1b[")) {
		t.Fatalf("gap-report --json must never be styled:\n%s", out)
	}
}

// extractProjectDir strips --project-dir in both forms from a DisableFlagParsing
// arg slice and returns the value + cleaned args.
func TestExtractProjectDir(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantVal  string
		wantArgs []string
	}{
		{"space form", []string{"--project-dir", "/foo", "config"}, "/foo", []string{"config"}},
		{"equals form", []string{"--project-dir=/bar", "config"}, "/bar", []string{"config"}},
		{"last wins", []string{"--project-dir=/a", "--project-dir=/b", "up"}, "/b", []string{"up"}},
		{"absent", []string{"config", "--services"}, "", []string{"config", "--services"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			val, args := extractProjectDir(tc.args)
			if val != tc.wantVal {
				t.Fatalf("val=%q want %q", val, tc.wantVal)
			}
			if len(args) != len(tc.wantArgs) {
				t.Fatalf("args=%v want %v", args, tc.wantArgs)
			}
			for i := range args {
				if args[i] != tc.wantArgs[i] {
					t.Fatalf("args[%d]=%q want %q", i, args[i], tc.wantArgs[i])
				}
			}
		})
	}
}
