package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// version subcommand output via the cobra OutOrStdout() wiring (spec §5).
// Line 1 is resolveVersion() (script-compat contract; equals the ldflags var on
// release builds, and a derived dev+<commit>[-dirty] string on plain go build).
// Line 2 is the linked compose-go version (transparency line; present whenever
// runtime/debug.ReadBuildInfo() resolves the dep, including in `go test`).
func TestVersionSubcommand(t *testing.T) {
	root := newRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	// assertion 1: first line == resolveVersion() (robust across build envs)
	want := resolveVersion()
	if lines[0] != want {
		t.Fatalf("version line 1 = %q, want resolveVersion()=%q", lines[0], want)
	}
	// assertion 2: second line starts with "compose-go " (linked-dep transparency)
	if len(lines) < 2 || !strings.HasPrefix(lines[1], "compose-go ") {
		t.Fatalf("version line 2 must start with \"compose-go \", got lines=%q", lines)
	}
}

// TestResolveVersion_LdflagsWins: when the package-level version var is set to a
// non-"dev" value (simulating an ldflags release stamp or Makefile git-describe),
// resolveVersion() must return it verbatim without querying build info.
func TestResolveVersion_LdflagsWins(t *testing.T) {
	orig := version
	version = "v1.2.3"
	defer func() { version = orig }()

	got := resolveVersion()
	if got != "v1.2.3" {
		t.Fatalf("resolveVersion() with version=%q = %q, want %q", version, got, "v1.2.3")
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
	for _, k := range []string{"COMPOSE_FILE", "COMPOSE_ENV_FILES", "CENVKIT_ENV", "WEB_PORT"} {
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

// ─── run command tests ────────────────────────────────────────────────────────

// runRun executes `run [args...]` via newRootCmd and returns (combined output, error).
func runRun(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"run", "--project-dir", dir}, args...))
	err := root.Execute()
	return out.String(), err
}

// TestRunCmd_DashDashRequired: `cenvkit run` without `--` exits 2.
func TestRunCmd_DashDashRequired(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".env"), "A=1\n")
	_, err := runRun(t, dir, "echo", "hi") // no `--` separator
	var ee *exitError
	if !errors.As(err, &ee) || ee.ExitCode() != 2 {
		t.Fatalf("run without -- want exitError code 2, got %v", err)
	}
}

// TestRunCmd_DashDashEmptyCommand: `cenvkit run --` with nothing after exits 2.
func TestRunCmd_DashDashEmptyCommand(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".env"), "A=1\n")
	_, err := runRun(t, dir, "--") // -- but no command
	var ee *exitError
	if !errors.As(err, &ee) || ee.ExitCode() != 2 {
		t.Fatalf("run -- <empty> want exitError code 2, got %v", err)
	}
}

// TestRunCmd_MissingBinary_Exit127: a nonexistent binary exits 127.
func TestRunCmd_MissingBinary_Exit127(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".env"), "A=1\n")
	_, err := runRun(t, dir, "--", "/no/such/binary/xyzzy_42")
	var ee *exitError
	if !errors.As(err, &ee) || ee.ExitCode() != 127 {
		t.Fatalf("missing binary want exitError code 127, got %v", err)
	}
}

// TestRunCmd_NonExecutable_Exit126: a file that exists but is not executable exits 126.
func TestRunCmd_NonExecutable_Exit126(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".env"), "A=1\n")
	// Write a plain file (no +x bit)
	nonExec := filepath.Join(dir, "notexec")
	mustWriteFile(t, nonExec, "#!/bin/sh\necho hi\n")
	// Ensure it is NOT executable (os.WriteFile with 0o644 — already non-exec)

	_, err := runRun(t, dir, "--", nonExec)
	var ee *exitError
	if !errors.As(err, &ee) || ee.ExitCode() != 126 {
		t.Fatalf("non-executable binary want exitError code 126, got %v (file: %s)", err, nonExec)
	}
}

// TestRunCmd_Print_ExitsZero: `--print` dumps chain env and exits 0 (no exec).
func TestRunCmd_Print_ExitsZero(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".env"), "CHAIN_KEY=chain_val\n")
	out, err := runRun(t, dir, "--print", "--", "false") // `false` would exit 1 if exec'd
	if err != nil {
		t.Fatalf("run --print want nil error, got %v\nout: %s", err, out)
	}
	// --print must have emitted the chain key
	if !strings.Contains(out, "CHAIN_KEY=") {
		t.Fatalf("run --print must emit CHAIN_KEY=, got:\n%s", out)
	}
}

// TestRunCmd_ChildExitCode: child exits with its own code, propagated via exitError.
func TestRunCmd_ChildExitCode(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".env"), "A=1\n")
	// sh -c 'exit 3' → child exits 3
	_, err := runRun(t, dir, "--", "sh", "-c", "exit 3")
	var ee *exitError
	if !errors.As(err, &ee) || ee.ExitCode() != 3 {
		t.Fatalf("child exit 3 want exitError code 3, got %v", err)
	}
}

// TestRunCmd_ChildSuccess_NilError: child exits 0 → err is nil.
func TestRunCmd_ChildSuccess_NilError(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".env"), "A=1\n")
	_, err := runRun(t, dir, "--", "sh", "-c", "exit 0")
	if err != nil {
		t.Fatalf("child exit 0 want nil error, got %v", err)
	}
}

// ─── env command tests ────────────────────────────────────────────────────────

// runEnvCmd executes `env [args...]` via newRootCmd and returns (combined output, error).
func runEnvCmd(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"env", "--project-dir", dir}, args...))
	err := root.Execute()
	return out.String(), err
}

// TestEnvCmd_Dotenv_Default: default format is dotenv; chain keys appear; shell-only keys absent.
func TestEnvCmd_Dotenv_Default(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".env"), "CHAIN_KEY=chain_val\n")
	// Unset CHAIN_KEY from process env to avoid shell-wins masking
	prev, had := os.LookupEnv("CHAIN_KEY")
	_ = os.Unsetenv("CHAIN_KEY")
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("CHAIN_KEY", prev)
		} else {
			_ = os.Unsetenv("CHAIN_KEY")
		}
	})

	out, err := runEnvCmd(t, dir)
	if err != nil {
		t.Fatalf("env (dotenv): %v\n%s", err, out)
	}
	if !strings.Contains(out, "CHAIN_KEY=") {
		t.Fatalf("env dotenv must emit CHAIN_KEY=, got:\n%s", out)
	}
}

// TestEnvCmd_JSON_Format: --format json produces a JSON object with chain keys.
func TestEnvCmd_JSON_Format(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".env"), "JSON_KEY=jval\n")
	prev, had := os.LookupEnv("JSON_KEY")
	_ = os.Unsetenv("JSON_KEY")
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("JSON_KEY", prev)
		} else {
			_ = os.Unsetenv("JSON_KEY")
		}
	})

	out, err := runEnvCmd(t, dir, "--format", "json")
	if err != nil {
		t.Fatalf("env --format json: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"JSON_KEY"`) {
		t.Fatalf("env --format json must contain JSON_KEY, got:\n%s", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("env --format json must never contain ANSI:\n%s", out)
	}
}

// TestEnvCmd_Shell_Format: --format shell produces `export KEY='value'` lines.
func TestEnvCmd_Shell_Format(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".env"), "SHELL_KEY=sval\n")
	prev, had := os.LookupEnv("SHELL_KEY")
	_ = os.Unsetenv("SHELL_KEY")
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("SHELL_KEY", prev)
		} else {
			_ = os.Unsetenv("SHELL_KEY")
		}
	})

	out, err := runEnvCmd(t, dir, "--format", "shell")
	if err != nil {
		t.Fatalf("env --format shell: %v\n%s", err, out)
	}
	if !strings.Contains(out, "export SHELL_KEY=") {
		t.Fatalf("env --format shell must emit 'export SHELL_KEY=', got:\n%s", out)
	}
}

// TestEnvCmd_EmptyChain_ExitsZero: no chain files → exit 0, no output.
func TestEnvCmd_EmptyChain_ExitsZero(t *testing.T) {
	dir := t.TempDir() // no .env, no chain file
	out, err := runEnvCmd(t, dir)
	if err != nil {
		t.Fatalf("env on empty chain must exit 0, got %v\n%s", err, out)
	}
}

// TestEnvCmd_NoExpand_LiteralDollar: --no-expand leaves ${VAR} unexpanded in output.
func TestEnvCmd_NoExpand_LiteralDollar(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".env"), "KEY=${SOME_VAR}\n")
	prev, had := os.LookupEnv("KEY")
	_ = os.Unsetenv("KEY")
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("KEY", prev)
		} else {
			_ = os.Unsetenv("KEY")
		}
	})

	out, err := runEnvCmd(t, dir, "--no-expand")
	if err != nil {
		t.Fatalf("env --no-expand: %v\n%s", err, out)
	}
	// With --no-expand the value "${SOME_VAR}" stays literal in the file value,
	// so Emit should encode it (dotenvQuote wraps in double-quotes + escapes $)
	if !strings.Contains(out, "KEY=") {
		t.Fatalf("env --no-expand must emit KEY=..., got:\n%s", out)
	}
	// The expansion must NOT have resolved SOME_VAR
	if strings.Contains(out, os.Getenv("SOME_VAR")) && os.Getenv("SOME_VAR") != "" {
		t.Fatalf("env --no-expand must not expand ${SOME_VAR}, got:\n%s", out)
	}
}

// TestEnvCmd_InvalidFormat: unknown --format returns an error.
func TestEnvCmd_InvalidFormat(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".env"), "A=1\n")
	_, err := runEnvCmd(t, dir, "--format", "csv")
	if err == nil {
		t.Fatal("env --format csv must error")
	}
}

// TestRunCmd_SignalExit128PlusSigno: a child killed by SIGTERM exits 128+15.
// We spawn a child that ignores SIGINT but is killed by SIGTERM after 50ms.
// The parent uses execChild directly (pkg main, white-box) so we can observe
// the returned exitError without going through os.Exit.
func TestRunCmd_SignalExit128PlusSigno(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".env"), "A=1\n")
	// sh sleeps indefinitely; we send SIGTERM via `kill` in a sibling goroutine.
	// Use execChild directly rather than going through cobra so we can inspect
	// the error without it hitting os.Exit in main().
	//
	// Strategy: a short-lived sh that ignores SIGINT and traps on SIGTERM.
	// We don't actually need to send the signal ourselves — `sh -c 'kill -TERM $$'`
	// makes the child kill itself with SIGTERM, which should come back as 128+15.
	err := execChild([]string{"sh", "-c", "kill -TERM $$"}, map[string]string{})
	var ee *exitError
	if !errors.As(err, &ee) {
		t.Fatalf("want exitError, got %T: %v", err, err)
	}
	const wantCode = 128 + 15 // SIGTERM = 15
	if ee.ExitCode() != wantCode {
		t.Fatalf("signal-killed child want exit %d (128+SIGTERM), got %d", wantCode, ee.ExitCode())
	}
}

// TestEnvCmd_QuoteRoundTrip_* tests verify that special characters in chain
// values survive dotenv Emit→parse safely. We check the raw dotenv output rather
// than re-parsing (avoids bootstrapping a second parser in tests), and we
// verify the dotenvQuote escaping rules match the production code's contract:
//   - space:   KEY="hello world"
//   - newline: KEY="line1\nline2"  (literal \n in output, not a real newline)
//   - double-quote: KEY="say \"hi\""
//   - dollar:  KEY="\$VAR"

// TestEnvCmd_Quote_Space: a value containing a space is double-quoted.
func TestEnvCmd_Quote_Space(t *testing.T) {
	dir := t.TempDir()
	// dotenv files support double-quoted values; compose-go parses them correctly.
	mustWriteFile(t, filepath.Join(dir, ".env"), "SPACE_KEY=\"hello world\"\n")
	prev, had := os.LookupEnv("SPACE_KEY")
	os.Unsetenv("SPACE_KEY") //nolint:errcheck
	t.Cleanup(func() {
		if had {
			os.Setenv("SPACE_KEY", prev) //nolint:errcheck
		} else {
			os.Unsetenv("SPACE_KEY") //nolint:errcheck
		}
	})

	out, err := runEnvCmd(t, dir)
	if err != nil {
		t.Fatalf("env (space val): %v\n%s", err, out)
	}
	// dotenvQuote wraps in double-quotes; the space stays literal (no escape needed).
	if !strings.Contains(out, `SPACE_KEY="hello world"`) {
		t.Fatalf("env dotenv must emit SPACE_KEY=\"hello world\", got:\n%s", out)
	}
}

// TestEnvCmd_Quote_DollarSign: a literal dollar in a value is \$-escaped in dotenv output.
func TestEnvCmd_Quote_DollarSign(t *testing.T) {
	dir := t.TempDir()
	// Use single-quoted dotenv value so compose-go treats $ literally on load.
	mustWriteFile(t, filepath.Join(dir, ".env"), "DOLLAR_KEY='pay $5'\n")
	prev, had := os.LookupEnv("DOLLAR_KEY")
	os.Unsetenv("DOLLAR_KEY") //nolint:errcheck
	t.Cleanup(func() {
		if had {
			os.Setenv("DOLLAR_KEY", prev) //nolint:errcheck
		} else {
			os.Unsetenv("DOLLAR_KEY") //nolint:errcheck
		}
	})

	out, err := runEnvCmd(t, dir)
	if err != nil {
		t.Fatalf("env (dollar val): %v\n%s", err, out)
	}
	// dotenvQuote escapes $ → \$ so compose-go does not re-expand on read.
	if !strings.Contains(out, `DOLLAR_KEY=`) {
		t.Fatalf("env dotenv must emit DOLLAR_KEY=, got:\n%s", out)
	}
	// The dollar must be escaped: \$ not a bare $
	if !strings.Contains(out, `\$`) {
		t.Fatalf("env dotenv must escape $ as \\$, got:\n%s", out)
	}
}

// TestEnvCmd_Quote_DoubleQuote: a double-quote in a value is \" in dotenv output.
func TestEnvCmd_Quote_DoubleQuote(t *testing.T) {
	dir := t.TempDir()
	// Use single-quoted dotenv value so compose-go treats the " literally on load.
	mustWriteFile(t, filepath.Join(dir, ".env"), `DQUOTE_KEY='say "hi"'`+"\n")
	prev, had := os.LookupEnv("DQUOTE_KEY")
	os.Unsetenv("DQUOTE_KEY") //nolint:errcheck
	t.Cleanup(func() {
		if had {
			os.Setenv("DQUOTE_KEY", prev) //nolint:errcheck
		} else {
			os.Unsetenv("DQUOTE_KEY") //nolint:errcheck
		}
	})

	out, err := runEnvCmd(t, dir)
	if err != nil {
		t.Fatalf("env (dquote val): %v\n%s", err, out)
	}
	if !strings.Contains(out, `DQUOTE_KEY=`) {
		t.Fatalf("env dotenv must emit DQUOTE_KEY=, got:\n%s", out)
	}
	// dotenvQuote escapes " → \"
	if !strings.Contains(out, `\"`) {
		t.Fatalf("env dotenv must escape \" as \\\", got:\n%s", out)
	}
}

// TestEnvCmd_Env_Flag_ReResolves: -e <env> re-resolves the chain to the requested tier.
// The chain default is "dev" (when CENVKIT_ENV is unset and .env has no CENVKIT_ENV= line).
// We use a "staging" tier that is NOT the default — without -e staging, .staging.env is
// absent from the chain; with -e staging, CENVKIT_ENV=staging is injected and it loads.
func TestEnvCmd_Env_Flag_ReResolves(t *testing.T) {
	dir := t.TempDir()
	// Base chain file (no CENVKIT_ENV= line → default tier is "dev")
	mustWriteFile(t, filepath.Join(dir, ".env"), "BASE_KEY=base_val\n")
	// Staging-tier chain file — only loaded when CENVKIT_ENV=staging (via -e staging)
	mustWriteFile(t, filepath.Join(dir, ".staging.env"), "STAGING_KEY=staging_val\n")
	for _, key := range []string{"BASE_KEY", "STAGING_KEY", "CENVKIT_ENV"} {
		prev, had := os.LookupEnv(key)
		os.Unsetenv(key) //nolint:errcheck
		func(k, pv string, hp bool) {
			t.Cleanup(func() {
				if hp {
					os.Setenv(k, pv) //nolint:errcheck
				} else {
					os.Unsetenv(k) //nolint:errcheck
				}
			})
		}(key, prev, had)
	}

	// Without -e: default tier (dev) → STAGING_KEY absent
	outBase, err := runEnvCmd(t, dir)
	if err != nil {
		t.Fatalf("env (no -e): %v\n%s", err, outBase)
	}
	if strings.Contains(outBase, "STAGING_KEY") {
		t.Fatalf("env without -e staging must NOT emit STAGING_KEY (staging chain not selected):\n%s", outBase)
	}

	// With -e staging: staging chain selected → STAGING_KEY present
	outStaging, err := runEnvCmd(t, dir, "-e", "staging")
	if err != nil {
		t.Fatalf("env -e staging: %v\n%s", err, outStaging)
	}
	if !strings.Contains(outStaging, "STAGING_KEY=") {
		t.Fatalf("env -e staging must emit STAGING_KEY= (staging tier chain selected):\n%s", outStaging)
	}
}

// TestEnvCmd_NoExpand_LiteralDollarInOutput: --no-expand keeps ${VAR} literal in the
// emitted dotenv output (dotenvQuote escapes the $ so it survives re-parse).
func TestEnvCmd_NoExpand_LiteralDollarInOutput(t *testing.T) {
	dir := t.TempDir()
	// Use single-quoted value so compose-go loads the $ as a literal on input.
	mustWriteFile(t, filepath.Join(dir, ".env"), "NOEXP_KEY='${SOME_VAR}'\n")
	for _, key := range []string{"NOEXP_KEY", "SOME_VAR"} {
		prev, had := os.LookupEnv(key)
		os.Unsetenv(key) //nolint:errcheck
		func(k, pv string, hp bool) {
			t.Cleanup(func() {
				if hp {
					os.Setenv(k, pv) //nolint:errcheck
				} else {
					os.Unsetenv(k) //nolint:errcheck
				}
			})
		}(key, prev, had)
	}

	out, err := runEnvCmd(t, dir, "--no-expand")
	if err != nil {
		t.Fatalf("env --no-expand: %v\n%s", err, out)
	}
	if !strings.Contains(out, "NOEXP_KEY=") {
		t.Fatalf("env --no-expand must emit NOEXP_KEY=, got:\n%s", out)
	}
	// In --no-expand mode, ParseOrderedLiteral returns "${SOME_VAR}" verbatim,
	// and dotenvQuote escapes $ → \$. The output must contain \$ (not be expanded).
	if !strings.Contains(out, `\$`) {
		t.Fatalf("env --no-expand dotenv output must contain \\$ (literal dollar escaped), got:\n%s", out)
	}
}

// TestExtractPersistentFlag: extractPersistentFlag strips --name VAL and --name=VAL
// (in any position, last-wins for repeated flags) for arbitrary flag names.
// Closes the no-docker gap on the compose --chain strip security contract:
// the acceptance C4-8a/b tests are docker-gated; this one runs without docker.
// Tests BOTH "chain" (the C4 named-chain selector) AND "project-dir" (existing seam).
// RED if a form is not stripped, the wrong value is returned, or non-flag args are lost.
func TestExtractPersistentFlag(t *testing.T) {
	tests := []struct {
		name     string
		flagName string
		args     []string
		wantVal  string
		wantArgs []string
	}{
		// --chain forms
		{
			name:     "chain space form mid-position",
			flagName: "chain",
			args:     []string{"compose", "--chain", "ci", "config"},
			wantVal:  "ci",
			wantArgs: []string{"compose", "config"},
		},
		{
			name:     "chain equals form",
			flagName: "chain",
			args:     []string{"--chain=api", "compose", "config"},
			wantVal:  "api",
			wantArgs: []string{"compose", "config"},
		},
		{
			name:     "chain last-wins on repeat",
			flagName: "chain",
			args:     []string{"--chain=ci", "--chain", "api", "config"},
			wantVal:  "api",
			wantArgs: []string{"config"},
		},
		{
			name:     "chain absent",
			flagName: "chain",
			args:     []string{"compose", "config"},
			wantVal:  "",
			wantArgs: []string{"compose", "config"},
		},
		// --project-dir forms (existing seam — cross-check extractPersistentFlag directly)
		{
			name:     "project-dir space form",
			flagName: "project-dir",
			args:     []string{"--project-dir", "/foo", "config"},
			wantVal:  "/foo",
			wantArgs: []string{"config"},
		},
		{
			name:     "project-dir equals form",
			flagName: "project-dir",
			args:     []string{"config", "--project-dir=/bar"},
			wantVal:  "/bar",
			wantArgs: []string{"config"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			val, got := extractPersistentFlag(tc.args, tc.flagName)
			if val != tc.wantVal {
				t.Fatalf("val=%q want %q", val, tc.wantVal)
			}
			if len(got) != len(tc.wantArgs) {
				t.Fatalf("args=%v want %v", got, tc.wantArgs)
			}
			for i := range got {
				if got[i] != tc.wantArgs[i] {
					t.Fatalf("args[%d]=%q want %q", i, got[i], tc.wantArgs[i])
				}
			}
		})
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
