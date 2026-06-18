# cenvkit C1 — `gap-report` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a daemon-free `cenvkit gap-report` CI/pre-build lint that exits non-zero when a service `env_file:`-only `${VAR}` falls back at the real run (the #3435 gap).

**Architecture:** Pure projection over the EXISTING gap detection. `engine.Provenance` already computes `VarTrace.Gap = referenced && !InChain && len(RuntimeDefs)>0` (internal/engine/provenance.go:424). C1 adds (a) a pure-Go `GapReport` projection + renderers in `internal/provenance` (the compose-go-free model package), and (b) a `gap-report` cobra command that resolves the chain, gates on `engine.HasComposeFileEnv`, calls `Provenance`, and maps the result to exit codes via a new `exitError` seam. No new gap logic, no compose-go added to `cmd`/`provenance`, no docker daemon.

**Tech Stack:** Go, cobra, compose-go v2.11.0 (already vendored; only `internal/engine` imports it), stdlib `encoding/json`.

## Global Constraints

- **Go:** `gofmt -l .` MUST be empty; `go vet ./...` clean; `go build ./...`; `go test ./... -count=1` green. (Verification gate per CLAUDE.md; the docker acceptance path is gated but gap-report itself is daemon-free.)
- **Seam:** `internal/provenance` imports NEITHER compose-go NOR `internal/engine` (CI seam check in `test/seam_test.go`). New file `gapreport.go` may import only stdlib.
- **Exit-code contract (spec §6):** `1` = one or more gaps; `0` = clean (compose file present, no gaps); `2` = no compose file discovered (misconfiguration — NOT a clean pass).
- **Daemon-free:** `gap-report` MUST NOT exec docker; it loads the compose model in-process via the existing `engine.Provenance`. Acceptance runs under `SMOKE_SKIP_DOCKER=1`.
- **JSON is never styled** (machine output ANSI-free regardless of `--color`).
- **Determinism:** all output is sorted (var name, then engine-sorted Effect order) for stable goldens.
- **Secrets:** NOT masked (out of scope) — gap output prints fallback values verbatim.
- **Git is the architect's.** Teammates do NOT run git; each task's "Commit" step marks a verified boundary the architect commits after the full gate passes on a frozen tree.

---

### Task 1: `GapReport` model + `CollectGaps` projection

**Files:**
- Create: `internal/provenance/gapreport.go`
- Test: `internal/provenance/gapreport_test.go`

**Interfaces:**
- Consumes: existing `provenance.Report`, `provenance.VarTrace` (fields `Gap bool`, `Effects []Effect`), `provenance.Effect` (fields `Service, Field, Resolved string`), and the existing unexported `stripVarPrefix(name, resolved string) string` (render.go:372, same package).
- Produces: `provenance.GapSite{Var, Service, Field, Fallback, Fix string}`, `provenance.GapReport{Gaps []GapSite; Count int}`, `func CollectGaps(r Report) GapReport`.

- [ ] **Step 1: Write the failing test**

```go
// internal/provenance/gapreport_test.go
package provenance

import (
	"reflect"
	"testing"
)

func TestCollectGaps(t *testing.T) {
	r := Report{Vars: map[string]VarTrace{
		// a real gap: referenced, not in chain, env_file-only — engine set Gap=true.
		"WEB_PORT": {
			Name: "WEB_PORT", Gap: true,
			Effects: []Effect{{Service: "web", Field: "ports[0]", Resolved: "0:80", Gap: true}},
		},
		// list-form leaf carries the "KEY=" prefix; Fallback must be normalized.
		"DB_HOST": {
			Name: "DB_HOST", Gap: true,
			Effects: []Effect{{Service: "api", Field: "environment[0]", Resolved: "DB_HOST=", Gap: true}},
		},
		// not a gap: in the chain — must be excluded.
		"IN_CHAIN": {Name: "IN_CHAIN", InChain: true,
			Effects: []Effect{{Service: "web", Field: "image", Resolved: "nginx"}}},
	}}
	got := CollectGaps(r)
	want := GapReport{Count: 2, Gaps: []GapSite{
		{Var: "DB_HOST", Service: "api", Field: "environment[0]", Fallback: "",
			Fix: "add DB_HOST to the Layer-1 chain (e.g. .env), or use it runtime-only"},
		{Var: "WEB_PORT", Service: "web", Field: "ports[0]", Fallback: "0:80",
			Fix: "add WEB_PORT to the Layer-1 chain (e.g. .env), or use it runtime-only"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CollectGaps mismatch\n got: %#v\nwant: %#v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/provenance/ -run TestCollectGaps -v`
Expected: FAIL — `undefined: CollectGaps` (and `GapReport`/`GapSite`).

- [ ] **Step 3: Write minimal implementation**

```go
// internal/provenance/gapreport.go
package provenance

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// GapSite is one place an env_file:-only ${VAR} reaches the compose model and so
// falls back at the real run (the #3435 gap). Field names mirror Effect
// (service/field); Fallback is the value the run interpolates (Effect.Resolved,
// human-normalized via stripVarPrefix).
type GapSite struct {
	Var      string `json:"var"`
	Service  string `json:"service"`
	Field    string `json:"field"`
	Fallback string `json:"fallback"`
	Fix      string `json:"fix"`
}

// GapReport is the gap-report projection of a Report: every gap site + a count.
type GapReport struct {
	Gaps  []GapSite `json:"gaps"`
	Count int       `json:"count"`
}

// CollectGaps projects a Report into its gap sites. Order is deterministic: var
// name, then the engine-sorted Effect order (service, field). A gap site is every
// Effect of a var whose Gap is true (engine already set Gap = referenced &&
// !InChain && len(RuntimeDefs)>0, provenance.go:424).
func CollectGaps(r Report) GapReport {
	names := make([]string, 0, len(r.Vars))
	for name := range r.Vars {
		names = append(names, name)
	}
	sort.Strings(names)
	gr := GapReport{}
	for _, name := range names {
		vt := r.Vars[name]
		if !vt.Gap {
			continue
		}
		fix := fmt.Sprintf("add %s to the Layer-1 chain (e.g. .env), or use it runtime-only", name)
		for _, e := range vt.Effects {
			gr.Gaps = append(gr.Gaps, GapSite{
				Var:      name,
				Service:  e.Service,
				Field:    e.Field,
				Fallback: stripVarPrefix(name, e.Resolved),
				Fix:      fix,
			})
		}
	}
	gr.Count = len(gr.Gaps)
	return gr
}

// RenderGapReportJSON / RenderGapReportHuman are added in Task 2.
var _ = json.Marshal
var _ io.Writer
```

(The two trailing `var _` lines keep the file compiling before Task 2 adds the renderers that use `json`/`io`; delete them in Task 2.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/provenance/ -run TestCollectGaps -v`
Expected: PASS.

- [ ] **Step 5: gofmt + commit (architect)**

```bash
gofmt -w internal/provenance/gapreport.go internal/provenance/gapreport_test.go
go vet ./internal/provenance/
git add internal/provenance/gapreport.go internal/provenance/gapreport_test.go
git commit -m "feat(provenance): GapReport projection (CollectGaps)"
```

---

### Task 2: gap-report renderers (human + JSON)

**Files:**
- Modify: `internal/provenance/gapreport.go` (add the two renderers; remove the Task-1 placeholder `var _` lines)
- Test: `internal/provenance/gapreport_test.go` (add render tests)

**Interfaces:**
- Consumes: `GapReport` (Task 1), the existing `Styler` interface + `st(Styler) Styler` nil-safe helper (render.go:68).
- Produces: `func RenderGapReportJSON(w io.Writer, gr GapReport) error`, `func RenderGapReportHuman(w io.Writer, gr GapReport, s Styler)`.

- [ ] **Step 1: Write the failing test**

```go
// append to internal/provenance/gapreport_test.go
import (
	"bytes"
	"strings"
)

func TestRenderGapReportJSON(t *testing.T) {
	gr := GapReport{Count: 1, Gaps: []GapSite{
		{Var: "WEB_PORT", Service: "web", Field: "ports[0]", Fallback: "0:80",
			Fix: "add WEB_PORT to the Layer-1 chain (e.g. .env), or use it runtime-only"},
	}}
	var b bytes.Buffer
	if err := RenderGapReportJSON(&b, gr); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{`"count": 1`, `"var": "WEB_PORT"`, `"service": "web"`,
		`"field": "ports[0]"`, `"fallback": "0:80"`, `""` == "" && `"fix":`} {
		_ = want
	}
	if !strings.Contains(got, `"count": 1`) || !strings.Contains(got, `"var": "WEB_PORT"`) {
		t.Fatalf("json missing fields:\n%s", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("json must never be styled:\n%s", got)
	}
}

func TestRenderGapReportHumanClean(t *testing.T) {
	var b bytes.Buffer
	RenderGapReportHuman(&b, GapReport{}, nil) // nil styler => plain
	if got := b.String(); got != "no env_file→interpolation gaps\n" {
		t.Fatalf("clean output = %q", got)
	}
}

func TestRenderGapReportHumanGaps(t *testing.T) {
	gr := GapReport{Count: 1, Gaps: []GapSite{
		{Var: "WEB_PORT", Service: "web", Field: "ports[0]", Fallback: "0:80"},
	}}
	var b bytes.Buffer
	RenderGapReportHuman(&b, gr, nil)
	got := b.String()
	if !strings.Contains(got, "⚠ gap: ${WEB_PORT} used in service web ports[0] resolves to \"0:80\"") {
		t.Fatalf("gap line missing:\n%s", got)
	}
	if !strings.Contains(got, "1 gap(s) found") {
		t.Fatalf("summary missing:\n%s", got)
	}
}
```

(Clean up the first test's `want` slice — keep only the two `strings.Contains` assertions; the slice literal above is illustrative, delete it when typing.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/provenance/ -run TestRenderGapReport -v`
Expected: FAIL — `undefined: RenderGapReportJSON` / `RenderGapReportHuman`.

- [ ] **Step 3: Write minimal implementation**

Remove the two placeholder `var _` lines from Task 1 and add:

```go
// RenderGapReportJSON writes the gap report as indented JSON (never styled).
func RenderGapReportJSON(w io.Writer, gr GapReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(gr)
}

// RenderGapReportHuman writes a human gap report: one ⚠ line per gap site plus a
// red summary, or a single green ok line when clean. nil styler => plain.
func RenderGapReportHuman(w io.Writer, gr GapReport, s Styler) {
	sty := st(s)
	if gr.Count == 0 {
		fmt.Fprintln(w, sty.Ok("no env_file→interpolation gaps"))
		return
	}
	for _, g := range gr.Gaps {
		fmt.Fprintf(w, "%s\n", sty.Gap(fmt.Sprintf(
			"⚠ gap: ${%s} used in service %s %s resolves to %q at the run (defined only in a service env_file:).",
			g.Var, g.Service, g.Field, g.Fallback)))
	}
	fmt.Fprintf(w, "%s\n", sty.Fail(fmt.Sprintf("%d gap(s) found", gr.Count)))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/provenance/ -run TestRenderGapReport -v`
Expected: PASS. Then `go test ./internal/provenance/ -count=1` (whole package green).

- [ ] **Step 5: gofmt + commit (architect)**

```bash
gofmt -w internal/provenance/gapreport.go internal/provenance/gapreport_test.go
git add internal/provenance/gapreport.go internal/provenance/gapreport_test.go
git commit -m "feat(provenance): gap-report human + JSON renderers"
```

---

### Task 3: `cenvkit gap-report` command + `exitError` exit-code seam

**Files:**
- Modify: `cmd/cenvkit/main.go` (add `exitError` type; honor it in `main()`; add `newGapReportCmd`; register it)
- Test: `cmd/cenvkit/main_test.go` (add command tests with a temp fixture)

**Interfaces:**
- Consumes: existing `resolveProjectDir`, `splitProfiles`, `envValue`, `currentStyler` (main.go); `chain.Resolve`/`chain.Input`/`chain.Result` (Files, Vars); `engine.HasComposeFileEnv(dir string, env []string) bool` (discover.go:85); `engine.New().Provenance(ctx, engine.ProvInput{...})` (provenance.go:135); `engine.ProvFile{Path, Layer}`, `engine.ProvInput{ProjectDir, Env, Profiles, EnvFiles}`; `provenance.CollectGaps`, `provenance.RenderGapReport{JSON,Human}`.
- Produces: `type exitError struct{ code int; msg string }` with `Error() string` + `ExitCode() int`; `func newGapReportCmd() *cobra.Command`.

- [ ] **Step 1: Write the failing test**

```go
// append to cmd/cenvkit/main_test.go
import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeGapFixture creates a project dir whose web service references ${WEB_PORT}
// (in ports) but defines WEB_PORT only in a service env_file: — a #3435 gap.
// If inChain is true, WEB_PORT is also put in the Layer-1 .env (so no gap).
func writeGapFixture(t *testing.T, inChain bool) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "compose.yaml"),
		"services:\n  web:\n    image: nginx\n    env_file: [web.env]\n    ports:\n      - \"${WEB_PORT:-0}:80\"\n")
	mustWrite(t, filepath.Join(dir, "web.env"), "WEB_PORT=8080\n")
	env := ""
	if inChain {
		env = "WEB_PORT=8080\n"
	}
	mustWrite(t, filepath.Join(dir, ".env"), env)
	return dir
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runGapReport executes `gap-report --project-dir <dir> [extra...]` against the
// root command and returns (stdout, exitErr). exitErr is nil on a clean exit.
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
	dir := writeGapFixture(t, false)
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
	dir := writeGapFixture(t, true) // WEB_PORT now in the Layer-1 chain
	out, err := runGapReport(t, dir)
	if err != nil {
		t.Fatalf("want clean exit (nil), got %v out=%s", err, out)
	}
}

func TestGapReportExitsTwoWithoutComposeFile(t *testing.T) {
	dir := t.TempDir() // no compose.yaml
	mustWrite(t, filepath.Join(dir, ".env"), "FOO=bar\n")
	_, err := runGapReport(t, dir)
	var ee *exitError
	if !errors.As(err, &ee) || ee.ExitCode() != 2 {
		t.Fatalf("want exitError code 2, got %v", err)
	}
}

func TestGapReportJSON(t *testing.T) {
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
```

NOTE for the implementer/qa: these tests assume a hermetic environment — ensure `COMPOSE_FILE`, `COMPOSE_ENV_FILES`, `WEB_PORT` are NOT inherited from the runner (use `t.Setenv` to clear if your CI sets them).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/cenvkit/ -run TestGapReport -v`
Expected: FAIL — `undefined: exitError` / `gap-report` is an unknown command.

- [ ] **Step 3: Write minimal implementation**

3a. Add the exit-code seam to `cmd/cenvkit/main.go` — the `exitError` type:

```go
// exitError carries a process exit code out of a command's RunE so main() can
// os.Exit with it. An empty msg means the command already wrote its own output
// (e.g. the gap report) and only the code should propagate.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }
func (e *exitError) ExitCode() int { return e.code }
```

3b. Make `main()` honor it (add `"errors"` to the import block):

```go
func main() {
	if err := newRootCmd().Execute(); err != nil {
		es := style.Resolve(colorFlagFromArgs(os.Args), os.Stderr)
		var ee *exitError
		if errors.As(err, &ee) {
			if ee.msg != "" {
				fmt.Fprintln(os.Stderr, es.ErrorMsg("cenvkit: "+ee.msg))
			}
			os.Exit(ee.code)
		}
		fmt.Fprintln(os.Stderr, es.ErrorMsg("cenvkit: "+err.Error()))
		os.Exit(1)
	}
}
```

3c. Add the command:

```go
func newGapReportCmd() *cobra.Command {
	var jsonOut bool
	c := &cobra.Command{
		Use:   "gap-report",
		Short: "Report env_file:->${VAR} interpolation gaps (CI/pre-build lint; daemon-free)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := resolveProjectDir(cmd)
			if err != nil {
				return err
			}
			cr, err := chain.Resolve(chain.Input{ProjectDir: dir, OSEnv: os.Environ(), Hostname: os.Hostname})
			if err != nil {
				return err
			}
			// No compose file => the lint cannot run; for a pre-build check that is a
			// misconfiguration, NOT a clean pass. Exit 2 (distinct from 1=gaps/0=clean).
			if !engine.HasComposeFileEnv(dir, cr.Vars) {
				return &exitError{code: 2, msg: "no compose file found in " + dir}
			}
			pf := make([]engine.ProvFile, 0, len(cr.Files))
			for _, f := range cr.Files {
				pf = append(pf, engine.ProvFile{Path: f, Layer: "layer1"})
			}
			profiles := splitProfiles(envValue(cr.Vars, "COMPOSE_PROFILES"))
			rep, err := engine.New().Provenance(cmd.Context(), engine.ProvInput{
				ProjectDir: dir, Env: cr.Vars, Profiles: profiles, EnvFiles: pf,
			})
			if err != nil {
				return err
			}
			gr := provenance.CollectGaps(rep)
			out := cmd.OutOrStdout()
			if jsonOut {
				if err := provenance.RenderGapReportJSON(out, gr); err != nil {
					return err
				}
			} else {
				provenance.RenderGapReportHuman(out, gr, currentStyler())
			}
			if gr.Count > 0 {
				return &exitError{code: 1} // gaps found; report already printed (no extra msg)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&jsonOut, "json", false, "machine-readable JSON")
	return c
}
```

3d. Register it in `newRootCmd()`:

```go
	root.AddCommand(newEnvFilesCmd(), newComposeCmd(), newValidateCmd(),
		newInitCmd(), newEnvDebugCmd(), newGapReportCmd())
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/cenvkit/ -run TestGapReport -v`
Expected: PASS (all four sub-tests). Then `go build ./...` and `go test ./cmd/cenvkit/ -count=1`.

- [ ] **Step 5: gofmt + commit (architect)**

```bash
gofmt -w cmd/cenvkit/main.go cmd/cenvkit/main_test.go
go vet ./...
git add cmd/cenvkit/main.go cmd/cenvkit/main_test.go
git commit -m "feat(cmd): cenvkit gap-report lint (exit 1/0/2) + exitError seam"
```

---

### Task 4: end-to-end acceptance (drives the built binary, daemon-free)

**Files:**
- Modify: `test/cenvkit-acceptance_test.go`

**Interfaces:**
- Consumes: the existing acceptance harness's helper that builds + invokes the `cenvkit` binary (reuse the suite's existing build/run helper; do NOT add a second binary builder). Runs under `SMOKE_SKIP_DOCKER=1` because gap-report never execs docker.

- [ ] **Step 1: Write the failing test**

Add a scenario that, in a temp project (or an `examples/monorepo` fixture variant) with a service `env_file:`-only `${VAR}` referenced in the YAML:
- runs `cenvkit gap-report` → asserts **exit code 1** and stdout contains the gapped var;
- runs `cenvkit gap-report --json` → asserts exit 1 and `"count": 1`;
- adds the var to the Layer-1 `.env`, re-runs → asserts **exit 0**;
- runs in a dir with no compose file → asserts **exit 2**.

Use the suite's existing exit-code capture (the harness already inspects `*exec.ExitError` for the `compose` scenarios — reuse that path). Match the fixture shape from Task 3 (`compose.yaml` + `web.env` + `.env`).

- [ ] **Step 2: Run to verify it fails**

Run: `SMOKE_SKIP_DOCKER=1 go test ./test/ -run GapReport -v`
Expected: FAIL — `gap-report` exit codes not yet asserted / fixture absent until Tasks 1-3 are in.

- [ ] **Step 3: Implement the scenario** (fixture + assertions per Step 1; no production code — Tasks 1-3 already provide the behavior).

- [ ] **Step 4: Run to verify it passes**

Run: `SMOKE_SKIP_DOCKER=1 go test ./test/ -run GapReport -v` → PASS.
Then the FULL gate: `gofmt -l .` (empty), `go vet ./...`, `go test ./... -count=1`, and the docker acceptance path (`go test ./test/...` with docker up) to confirm nothing else regressed.

- [ ] **Step 5: gofmt + commit (architect)**

```bash
gofmt -w test/cenvkit-acceptance_test.go
git add test/cenvkit-acceptance_test.go
git commit -m "test(acceptance): gap-report exit 1/0/2 (daemon-free)"
```

---

## Self-Review

**Spec coverage (§6 Cycle 1):**
- daemon-free lint over the existing gap set (provenance.go:424) → Tasks 1+3 (CollectGaps reads `VarTrace.Gap`; command never execs docker). ✓
- standalone `gap-report` verb sharing one JSON schema with env-debug → Task 3 (verb) + Task 1 (`GapSite` reuses Effect field names service/field + resolved-as-fallback). ✓
- exit 1 / 0 / 2 → Task 3 (`exitError` + `HasComposeFileEnv` gate); asserted in Tasks 3+4. ✓
- stable `--json` → Tasks 1-2 (`GapReport`/`GapSite` JSON tags; renderer; never styled). ✓
- NOT in C1: run/env/envmap, rename, named chains, the env-debug `--chain` flag rename — none appear. ✓

**Placeholder scan:** every code step contains complete code; the two illustrative-cleanup notes (Task 2 Step 1 `want` slice; Task 1 placeholder `var _`) are explicitly called out, not left as TODO. ✓

**Type consistency:** `GapSite`/`GapReport`/`CollectGaps`/`RenderGapReportJSON`/`RenderGapReportHuman` names identical across Tasks 1-3; `exitError{code,msg}` + `ExitCode()` consistent between definition (3a) and assertions (Task 3 test); `engine.HasComposeFileEnv(dir, cr.Vars)` matches discover.go:85; `engine.ProvInput` fields match provenance.go:26. ✓

## Execution Handoff (compose-envkit team flow)

This plan does NOT use the generic subagent-driven options verbatim — it runs through the project's plan-gated **Agent Team** (per `.claude/TEAM.md`):
1. **Plan gate:** a fresh plan-mode **go-engineer** (Opus) reviews this plan read-only against the code, raises any contract issue, architect approves.
2. **Build:** go-engineer implements the production steps (Tasks 1-3 prod code: `internal/provenance/gapreport.go`, `cmd/cenvkit/main.go`); **qa-engineer** (Sonnet) authors the test steps (Tasks 1-4 `*_test.go` + acceptance). Output-changing? No — `gap-report` is a NEW command (purely additive), so qa test-authoring and go-engineer impl may run in parallel.
3. **Review:** **code-reviewer** (Opus) — focus on the exit-code contract, the seam (provenance stays compose-go-free), and JSON stability.
4. **Integrate:** architect runs the full gate on the FROZEN tree (`git stash -u && go test ./... -count=1 && stash pop` if staging a subset during flux), then commits/pushes (the per-task "Commit" steps are architect-owned boundaries).
