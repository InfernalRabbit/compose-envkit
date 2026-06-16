# cenvkit v1 ("thin") Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the frozen POSIX-`sh` engine of compose-envkit with a Go CLI, `cenvkit`, that assembles `COMPOSE_ENV_FILES` via Docker's own loader (`compose-go/v2`) and `exec`s `docker compose`, passing the ported acceptance suite (**60** assertions — see Task 7 Step 6; the verified `smoke-monorepo` baseline is 61, minus the dropped depth-knob assertion 11.2) with the deliberate upstream-driven inversions encoded.

**Architecture:** A cobra CLI (`cmd/cenvkit`) wires four focused packages: `internal/chain` (Layer-1 `.docker-env-chain` parse + token substitution, pure Go), `internal/engine` (Layer-2 env_file enumeration — the ONLY package importing compose-go, behind a fakeable `Engine` interface), `internal/debug` (env-debug modes over compose-go-free projections), and `internal/bootstrap` (`cenvkit init`). The two layers merge into one ordered, deduped `COMPOSE_ENV_FILES` (Layer-1 first, secrets last within Layer-1, Layer-2 after), which `docker compose` then loads as the interpolation context.

**Tech Stack:** Go 1.26 · `github.com/compose-spec/compose-go/v2 v2.11.0` (pinned) · `github.com/spf13/cobra` · table-driven `testing` · the existing `examples/monorepo/` fixture + ported `test/smoke*.sh` as the acceptance gate.

---

## Source documents (read before executing)

- **Spec (authoritative):** `docs/superpowers/specs/2026-06-15-cenvkit-go-rewrite-design.md`
- **compose-go API (go-doc verified):** `.claude/artifacts/compose-go-api.md`
- **D1 lever (go-doc + live-probe verified):** `.claude/artifacts/compose-go-d1-lever.md`
- **Acceptance port mapping (per-assertion):** `.claude/artifacts/acceptance-port-plan.md`
- **Spec audit (C1/C2/W1–W5/S1–S4):** `.claude/artifacts/spec-audit.md`
- **Legacy reference (do NOT modify):** `lib/compose-env.sh`, `lib/parse-compose-env-files.sh`, `bin/docker`, `templates/init.sh`, `test/smoke.sh`, `test/smoke-monorepo.sh`, `test/lint.sh`.

## Conventions & decisions baked into this plan

- **Module path:** `MODULE = github.com/InfernalRabbit/compose-envkit` (no git remote is configured yet; this only affects the public `go install …@latest` path, not local or vendored use). **Confirm with the maintainer before Task 1**; if it changes, it is a single `go mod init` argument and the `cmd/cenvkit/main.go` import path in Task 6. Everything else is module-relative.
- **compose-go pin:** EXACTLY `v2.11.0` (the `/v2` module — never the transitive v1.x). Verified present via `go doc` + live `LoadProject` probe (see D1 artifact). Bump only deliberately + re-run acceptance (spec §10).
- **Engine seam (spec §3, D3):** `internal/engine` is the ONLY package that imports compose-go. `chain`/`debug`/`cmd`/`bootstrap` import ZERO compose-go. `go list` (NO `-deps`; first-party packages only) is asserted in Task 3 to lock this.
- **D1 (user-confirmed):** lenient at assembly, upstream at runtime. The enumeration load uses `cli.WithoutEnvironmentResolution` so a missing *required* env_file does not abort the load; the engine then `os.Stat`-filters non-existent paths out of `COMPOSE_ENV_FILES`; the real `docker compose` run (loaded WITHOUT the lever) re-enforces `required:`.
- **Determinism:** `types.Project.Services` is a Go map → the engine MUST sort (service name, then env_file order within a service) before emitting. This is a contract the acceptance suite pins.
- **TDD:** every behavior gets a failing test first. Commit after each green step.
- **Safety rules carried from the sh kit:** no `sudo`, no `chmod 777`, never persist secrets; secrets file stays LAST within Layer-1. `chain` whitelists the host/env charset to `[A-Za-z0-9._-]` (kills the legacy sed-injection class — see W1).

## Execution & ownership mapping (team protocol)

This plan is written in canonical TDD form (test step + impl step interleaved). When executed by the **compose-envkit Agent Team**, map per the module boundaries in `CLAUDE.md`:
- **`*_test.go` steps → qa-engineer.** **production-code steps → go-engineer.** **all `git` + integration → architect (lead).**
- The lead seeds the team task-list from the **Task dependency graph** below, setting `owner` at creation and `blockedBy` per the graph. Within a single package task, qa writes the RED test, DMs the lead, go-engineer makes it GREEN — interleave via the mailbox; do not split a package into two unsynchronized tasks.

### Task dependency graph (for `TaskCreate` `blockedBy`)

```
T1 scaffold ───┬──> T2 chain ──────┐
               ├──> T3 engine ─────┼──> T4 envfiles ──┐
               └──> T5 bootstrap ──┘                  ├──> T6 debug+cmd wiring ──> T7 acceptance ──> T8 dist ──> T9 docs/flip
                       (T2 ─┐                          │
                        T3 ─┴─────────────────────────>┘  T6 also blockedBy T2,T3,T5)
```

- **T1** scaffold — no deps.
- **T2** chain — blockedBy T1.
- **T3** engine — blockedBy T1.
- **T4** envfiles (merge) — blockedBy T2, T3.
- **T5** bootstrap — blockedBy T1 (parallel with T2/T3).
- **T6** debug + cmd wiring — blockedBy T2, T3, T4, T5.
- **T7** acceptance port — blockedBy T6.
- **T8** distribution — blockedBy T6.
- **T9** docs + flip default — blockedBy T7.

---

## File structure

| Path | Responsibility | compose-go? |
|---|---|---|
| `go.mod`, `go.sum` | module `MODULE`, Go 1.26, pins | — |
| `cmd/cenvkit/main.go` | cobra entry; subcommand wiring; `exec docker compose` | no |
| `cmd/cenvkit/main_test.go` | qa-owned `package main` test: `version` output + `--project-dir` flag-set branch of `resolveProjectDir` | no |
| `internal/chain/chain.go` | Layer-1: resolve ENV/HOST, parse `.docker-env-chain`, substitute tokens, filter existing files, build the seed env (`Vars`) | no |
| `internal/chain/chain_test.go` | table-driven chain tests incl. W1 sanitization RED test | no |
| `internal/engine/engine.go` | `Input`/`Result`/`ProjectView` + `Engine` interface + `New()` compose-go impl | **YES (only here)** |
| `internal/engine/engine_test.go` | fixture-driven `Resolve` tests (D1, determinism, dedup, profiles) | no (uses real engine over fixtures) |
| `internal/engine/discover.go` | `HasComposeFile(dir, composeFileEnv)` existence check (chain-only fallback, G4); shares the `resolveComposeFiles` resolver with `Resolve` | yes |
| `internal/engine/discover_test.go` | qa-owned table tests for `HasComposeFile` (separator/interpolation/COMPOSE_FILE-present branch) | yes |
| `internal/debug/debug.go` | env-debug modes over `chain.Result` + `engine.ProjectView` + a tiny dotenv merge | no |
| `internal/debug/debug_test.go` | `--value`/`--trace`/`--chain`/`--files` tests | no |
| `internal/bootstrap/bootstrap.go` | `cenvkit init`: seed `.X` from `example.X` no-clobber, fan out to subdirs | no |
| `internal/bootstrap/bootstrap_test.go` | seeding + no-clobber (W5 RED) + fan-out + idempotency | no |
| `internal/envfiles/assemble.go` | merge Layer-1 + Layer-2 → ordered deduped `COMPOSE_ENV_FILES`; the `,`-separator guard | no |
| `internal/envfiles/assemble_test.go` | ordering/dedup/secrets-last (W3) + `,` guard tests | no |
| `test/cenvkit-acceptance_test.go` | Go-driven port of the smoke suites (build the binary, run subcommands) | no |
| `test/seam_test.go` | qa-owned white-box seam test (`package acceptance`): `chain.Resolve()` → `engine.Resolve()`, asserts merged `COMPOSE_ENV_FILES` ordering | no |
| `cenvkit` (POSIX shim) | vendored-mode `go run ./cmd/cenvkit "$@"` | — |
| `.goreleaser.yaml`, `.github/workflows/ci.yml` | distribution + CI | — |

---

## Task 1: Module scaffold + cobra skeleton

**Owner:** go-engineer. **Depends on:** none.

**Files:**
- Create: `go.mod`, `cmd/cenvkit/main.go`

- [ ] **Step 1: Confirm the module path** (one-time, with maintainer). Default `github.com/InfernalRabbit/compose-envkit`. Used verbatim below as `MODULE`.

- [ ] **Step 2: Initialize the module and add cobra**

```bash
cd /Users/infernal_rabbit/Workflow/Big/compose-envkit
go mod init github.com/InfernalRabbit/compose-envkit
go get github.com/spf13/cobra@latest
```
Expected: `go.mod` created with `go 1.26` (or current toolchain) and a `require github.com/spf13/cobra` line.

- [ ] **Step 3: Write the minimal cobra root with a `version` subcommand**

`cmd/cenvkit/main.go`:
```go
// Command cenvkit assembles COMPOSE_ENV_FILES from a layered env chain and
// execs `docker compose`. See docs/superpowers/specs for the design.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is overridden at release time via -ldflags "-X main.version=...".
var version = "dev"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "cenvkit",
		Short:         "Layered env-file assembly for Docker Compose",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().String("project-dir", "", "project directory (default: current directory)")
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the cenvkit version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), version)
			return nil
		},
	})
	return root
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "cenvkit:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 3b: Write the failing `cmd/cenvkit/main_test.go` RED test** (TDD invariant, line 29 — `version` is a spec §5 contract behavior; `--project-dir` wiring is depended on by `resolveProjectDir`). **Owner: qa-engineer** (test file); go-engineer keeps the wiring GREEN.

`cmd/cenvkit/main_test.go` (`package main`):
```go
package main

import (
	"bytes"
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
```
RED: the test fails to build with `undefined: resolveProjectDir` until Task 6 Step 5 adds the helper. Sequence: write `main_test.go` here → `TestVersionSubcommand` goes GREEN after Step 3 wiring; `TestProjectDirFlagWiring`'s `resolveProjectDir` branch goes GREEN once Task 6 Step 5 lands. (If go-engineer prefers to keep Task 1 to version-only, move the `resolveProjectDir`-branch sub-assertion into Task 6 Step 5's test slot — but keep both behaviors test-pinned.)

- [ ] **Step 4: Build, vet, format — verify it compiles and runs**

Run:
```bash
go build ./... && go vet ./... && gofmt -l . && go run ./cmd/cenvkit version
```
Expected: no build/vet output, `gofmt -l .` prints nothing, `go run … version` prints `dev`.

- [ ] **Step 5: Commit** (lead does the actual commit; go-engineer reports hash + `git diff --stat`)

```bash
git add go.mod go.sum cmd/cenvkit/main.go cmd/cenvkit/main_test.go
git commit -m "feat(cenvkit): module scaffold + cobra root with version"
```

---

## Task 2: `internal/chain` — Layer-1 chain (pure Go)

**Owner:** go-engineer (impl) + qa-engineer (tests). **Depends on:** T1.

**Responsibility:** Resolve `ComposeEnv` and `Host`; read `.docker-env-chain` (or built-in defaults); substitute the four tokens; keep only existing files in chain order (deduped); and build `Vars` (the `"K=V"` seed env for the engine, OS-env-wins).

**Files:**
- Create: `internal/chain/chain.go`
- Test: `internal/chain/chain_test.go`

**Contract (types — used verbatim by later tasks):**
```go
package chain

type Input struct {
	ProjectDir string                 // absolute project directory
	OSEnv      []string               // os.Environ(); injected for testability
	Hostname   func() (string, error) // injected; production passes os.Hostname
}

type Result struct {
	Files      []string // ordered absolute Layer-1 paths, existing only, deduped
	Vars       []string // merged "K=V" seed for the engine (OS env wins over file vars), sorted
	ComposeEnv string   // resolved COMPOSE_ENV ("dev" default)
	Host       string   // resolved + sanitized host ([A-Za-z0-9._-])
}

func Resolve(in Input) (Result, error)
```

- [ ] **Step 1: Write the failing token-sanitization test (W1 guard — RED on a naive impl)**

`internal/chain/chain_test.go`:
```go
package chain

import (
	"os"
	"path/filepath"
	"strings" // used by TestChainOrderingAndEnvSwitch (Step 5); declared here so Step 6 compiles
	"testing"
)

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHostSanitization_NoInjectionNoSplit(t *testing.T) {
	dir := t.TempDir()
	// chain references a host-named file; the resolved name must be sanitized.
	writeFile(t, dir, ".docker-env-chain", ".env\n.${HOST}.env\n.secrets.env\n")
	writeFile(t, dir, ".env", "BASE=1\n")
	writeFile(t, dir, ".evlhost.env", "H=1\n") // sanitized target
	writeFile(t, dir, ".secrets.env", "S=1\n")

	res, err := Resolve(Input{
		ProjectDir: dir,
		OSEnv:      []string{"HOSTNAME=ev|l&host"}, // sed-special chars
		Hostname:   func() (string, error) { return "ev|l&host", nil },
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Host != "evlhost" {
		t.Fatalf("Host = %q, want %q", res.Host, "evlhost")
	}
	// The resolved file must be .evlhost.env and exist; no entry may contain a comma.
	wantHostFile := filepath.Join(dir, ".evlhost.env")
	found := false
	for _, f := range res.Files {
		if f == wantHostFile {
			found = true
		}
		for _, c := range f {
			if c == ',' {
				t.Fatalf("resolved path contains comma (would split COMPOSE_ENV_FILES): %q", f)
			}
		}
	}
	if !found {
		t.Fatalf("did not resolve .evlhost.env; Files=%v", res.Files)
	}
}

func TestCommaInComposeEnv_DoesNotSplit(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".docker-env-chain", ".env\n.${ENV}.env\n")
	writeFile(t, dir, ".env", "BASE=1\n")
	writeFile(t, dir, ".ab.env", "X=1\n") // "a,b" sanitized -> "ab"
	res, err := Resolve(Input{
		ProjectDir: dir,
		OSEnv:      []string{"COMPOSE_ENV=a,b"},
		Hostname:   func() (string, error) { return "h", nil },
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.ComposeEnv != "ab" {
		t.Fatalf("ComposeEnv sanitized = %q, want %q", res.ComposeEnv, "ab")
	}
}
```

- [ ] **Step 2: Run the tests — verify they FAIL (no impl yet)**

Run: `go test ./internal/chain/ -run 'Sanitization|Comma' -v`
Expected: FAIL — `undefined: Resolve` (build error) or assertion failures.

- [ ] **Step 3: Implement `internal/chain/chain.go`**

```go
// Package chain resolves Layer-1: the .docker-env-chain file list plus the
// "K=V" seed environment for the engine. Pure Go — imports no compose-go.
package chain

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// defaultChain is used when no .docker-env-chain file is present (spec §4 step 2).
var defaultChain = []string{".env", ".${COMPOSE_ENV}.env", ".secrets.env"}

// sanitizeToken keeps only [A-Za-z0-9._-]; everything else is dropped. This kills
// the legacy sed-injection class and prevents a "," (the COMPOSE_ENV_FILES
// separator) or path-traversal char from entering a resolved path (audit W1).
func sanitizeToken(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	return b.String()
}

func osEnvMap(osEnv []string) map[string]string {
	m := make(map[string]string, len(osEnv))
	for _, kv := range osEnv {
		if i := strings.IndexByte(kv, '='); i > 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

// parseDotEnv is a minimal KEY=VALUE reader (skip blank / #comment lines; strip a
// single pair of surrounding quotes). The authoritative parse happens later in
// compose-go; this only seeds interpolation, so it is intentionally small.
func parseDotEnv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.IndexByte(line, '=')
		if i <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:i])
		v := strings.TrimSpace(line[i+1:])
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		out[k] = v
	}
	return out, sc.Err()
}

func resolveComposeEnv(in Input, osEnv map[string]string) string {
	if v := osEnv["COMPOSE_ENV"]; v != "" {
		return sanitizeToken(v)
	}
	// fall back to a COMPOSE_ENV= line in the root .env
	if m, err := parseDotEnv(filepath.Join(in.ProjectDir, ".env")); err == nil {
		if v := m["COMPOSE_ENV"]; v != "" {
			return sanitizeToken(v)
		}
	}
	return "dev"
}

func resolveHost(in Input, osEnv map[string]string) string {
	if v := osEnv["HOSTNAME"]; v != "" {
		return sanitizeToken(v)
	}
	if in.Hostname != nil {
		if h, err := in.Hostname(); err == nil {
			return sanitizeToken(h)
		}
	}
	if h, err := os.Hostname(); err == nil {
		return sanitizeToken(h)
	}
	return ""
}

func substituteTokens(tmpl, composeEnv, host string) string {
	r := strings.NewReplacer(
		"${ENV}", composeEnv,
		"${COMPOSE_ENV}", composeEnv,
		"${HOST}", host,
		"${HOSTNAME}", host,
	)
	return r.Replace(tmpl)
}

func readChainTemplates(projectDir string) ([]string, error) {
	f, err := os.Open(filepath.Join(projectDir, ".docker-env-chain"))
	if os.IsNotExist(err) {
		return append([]string(nil), defaultChain...), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read .docker-env-chain: %w", err)
	}
	defer f.Close()
	var tmpls []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tmpls = append(tmpls, line)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan .docker-env-chain: %w", err)
	}
	return tmpls, nil
}

// Resolve computes the Layer-1 file list and the seed environment.
func Resolve(in Input) (Result, error) {
	osEnv := osEnvMap(in.OSEnv)
	composeEnv := resolveComposeEnv(in, osEnv)
	host := resolveHost(in, osEnv)

	tmpls, err := readChainTemplates(in.ProjectDir)
	if err != nil {
		return Result{}, err
	}

	var files []string
	seen := map[string]bool{}
	fileVars := map[string]string{}
	for _, t := range tmpls {
		name := substituteTokens(t, composeEnv, host)
		if strings.ContainsRune(name, ',') {
			return Result{}, fmt.Errorf("resolved chain entry %q contains a comma (COMPOSE_ENV_FILES separator)", name)
		}
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(in.ProjectDir, name)
		}
		if _, statErr := os.Stat(path); statErr != nil {
			continue // skip-missing parity with the sh kit
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		files = append(files, path)
		if m, perr := parseDotEnv(path); perr == nil {
			for k, v := range m { // later files win (chain order)
				fileVars[k] = v
			}
		}
	}

	// Build Vars: file vars first, then OS env overlays (shell wins).
	merged := map[string]string{}
	for k, v := range fileVars {
		merged[k] = v
	}
	for k, v := range osEnv {
		merged[k] = v
	}
	if _, ok := merged["COMPOSE_ENV"]; !ok {
		merged["COMPOSE_ENV"] = composeEnv
	}
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	vars := make([]string, 0, len(keys))
	for _, k := range keys {
		vars = append(vars, k+"="+merged[k])
	}

	return Result{Files: files, Vars: vars, ComposeEnv: composeEnv, Host: host}, nil
}
```

- [ ] **Step 4: Run the W1 tests — verify they PASS**

Run: `go test ./internal/chain/ -run 'Sanitization|Comma' -v`
Expected: PASS.

- [ ] **Step 5: Add the remaining chain table tests** (port of smoke scenarios 12, 13, 17.1–17.2, 23 + smoke.sh 5.6 logic at the unit level)

`internal/chain/chain_test.go` (append):
```go
func TestChainOrderingAndEnvSwitch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".docker-env-chain", ".env\n.${ENV}.env\n.${HOSTNAME}.env\n.secrets.env\n")
	writeFile(t, dir, ".env", "COMPOSE_ENV=dev\nROOT=1\n")
	writeFile(t, dir, ".dev.env", "TIER=dev\n")
	writeFile(t, dir, ".prod.env", "TIER=prod\n")
	writeFile(t, dir, ".testhost.env", "H=1\n")
	writeFile(t, dir, ".secrets.env", "SECRET=xyz\n")

	tests := []struct {
		name      string
		osEnv     []string
		wantOrder []string // basenames, in order
		wantEnv   string
	}{
		{"dev default", []string{"HOSTNAME=testhost"},
			[]string{".env", ".dev.env", ".testhost.env", ".secrets.env"}, "dev"},
		{"prod via shell", []string{"COMPOSE_ENV=prod", "HOSTNAME=testhost"},
			[]string{".env", ".prod.env", ".testhost.env", ".secrets.env"}, "prod"},
		{"non-matching host drops .testhost.env", []string{"HOSTNAME=otherhost"},
			[]string{".env", ".dev.env", ".secrets.env"}, "dev"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Resolve(Input{ProjectDir: dir, OSEnv: tc.osEnv,
				Hostname: func() (string, error) { return "fallback", nil }})
			if err != nil {
				t.Fatal(err)
			}
			if res.ComposeEnv != tc.wantEnv {
				t.Fatalf("ComposeEnv=%q want %q", res.ComposeEnv, tc.wantEnv)
			}
			var got []string
			for _, f := range res.Files {
				got = append(got, filepath.Base(f))
			}
			if strings.Join(got, ",") != strings.Join(tc.wantOrder, ",") {
				t.Fatalf("order=%v want %v", got, tc.wantOrder)
			}
			// secrets must be last whenever present
			if got[len(got)-1] != ".secrets.env" {
				t.Fatalf("secrets not last: %v", got)
			}
		})
	}
}

func TestHostTokenEqualsHostnameToken(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".docker-env-chain", ".env\n.${HOST}.env\n")
	writeFile(t, dir, ".env", "B=1\n")
	writeFile(t, dir, ".testhost.env", "H=1\n")
	res, err := Resolve(Input{ProjectDir: dir, OSEnv: []string{"HOSTNAME=testhost"},
		Hostname: func() (string, error) { return "x", nil }})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(res.Files[len(res.Files)-1]) != ".testhost.env" {
		t.Fatalf("${HOST} did not resolve to .testhost.env: %v", res.Files)
	}
}
```

- [ ] **Step 6: Run all chain tests, vet, fmt**

Run: `go test ./internal/chain/ -v && go vet ./internal/chain/ && gofmt -l internal/chain/`
Expected: all PASS; no vet/fmt output.

- [ ] **Step 7: Commit**

```bash
git add internal/chain/chain.go internal/chain/chain_test.go
git commit -m "feat(chain): Layer-1 chain resolve + token sanitization (W1 guard)"
```

---

## Task 3: `internal/engine` — Layer-2 via compose-go (the seam)

**Owner:** go-engineer (impl) + qa-engineer (tests). **Depends on:** T1.

**Responsibility:** Load the compose project (include-aware, interpolated, profile-filtered) using the verified D1 lever, enumerate the ACTIVE set of env_file paths, `os.Stat`-filter missing ones, and emit a deterministic, deduped absolute path list plus a compose-go-free `ProjectView`.

**Files:**
- Create: `internal/engine/engine.go`, `internal/engine/discover.go`
- Test: `internal/engine/engine_test.go`

**Contract (types — used verbatim by `cmd` and `debug`):**
```go
package engine

import "context"

type Input struct {
	ProjectDir  string   // absolute working dir
	ConfigFiles []string // explicit -f; empty => COMPOSE_FILE / default discovery
	Env         []string // chain.Result.Vars — seeds interpolation
	Profiles    []string // active profiles (M3)
}

type ProjectView struct {
	WorkingDir string
	Services   map[string][]string // service -> existing resolved env_file abs paths
}

type Result struct {
	EnvFiles []string    // absolute, existing, active-only, deterministically ordered, deduped
	Project  ProjectView
}

type Engine interface {
	Resolve(ctx context.Context, in Input) (Result, error)
}

func New() Engine
```

- [ ] **Step 1: Pin compose-go and lock the API**

```bash
go get github.com/compose-spec/compose-go/v2@v2.11.0
go list -m github.com/compose-spec/compose-go/v2   # expect: v2.11.0
```
Expected: the exact line `github.com/compose-spec/compose-go/v2 v2.11.0`.

- [ ] **Step 2: Write the D1 failing test (RED — missing *required* env_file must NOT abort enumeration, and must be filtered out)**

`internal/engine/engine_test.go`:
```go
package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/InfernalRabbit/compose-envkit/internal/engine"
)

func write(t *testing.T, dir, rel, body string) string {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func basenames(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = filepath.Base(p)
	}
	sort.Strings(out)
	return out
}

// D1: a service with a missing *required* env_file must still load (lenient
// enumeration) and the missing path must be dropped from the result.
func TestResolve_MissingRequiredEnvFile_LenientAndFiltered(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "compose.yaml", `
services:
  web:
    image: busybox
    env_file:
      - ./present.env
      - path: ./MISSING.env
        required: true
`)
	write(t, dir, "present.env", "WEB=1\n")
	// MISSING.env intentionally absent.

	res, err := engine.New().Resolve(context.Background(), engine.Input{ProjectDir: dir})
	if err != nil {
		t.Fatalf("enumeration must be lenient, got error: %v", err)
	}
	got := basenames(res.EnvFiles)
	if len(got) != 1 || got[0] != "present.env" {
		t.Fatalf("EnvFiles = %v, want [present.env] (MISSING.env filtered)", got)
	}
}
```

- [ ] **Step 3: Run it — verify it FAILS**

Run: `go test ./internal/engine/ -run MissingRequired -v`
Expected: FAIL — `undefined: engine.New` (build error).

- [ ] **Step 4: Implement `internal/engine/engine.go`** (uses the verified lever `cli.WithoutEnvironmentResolution` — see `.claude/artifacts/compose-go-d1-lever.md`)

```go
// Package engine wraps compose-go (v2.11.0). It is the ONLY package in cenvkit
// that imports compose-go; everything else consumes the plain-Go Result/ProjectView.
package engine

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/compose-spec/compose-go/v2/cli"
)

type composeEngine struct{}

// New returns the compose-go-backed Engine, pinned to v2.11.0.
func New() Engine { return &composeEngine{} }

func (e *composeEngine) Resolve(ctx context.Context, in Input) (Result, error) {
	// COMPOSE_FILE selection + ${VAR} interpolation is the cenvkit-side seam:
	// cli.WithConfigFileEnv reads COMPOSE_FILE from the (empty-until-WithEnv) load
	// env, splits on COMPOSE_PATH_SEPARATOR-else-os.PathListSeparator, and os.Stats
	// the RAW string with NO interpolation (probe-verified, compose-go v2.11.0 —
	// see .claude/artifacts/compose-go-d1-lever.md). So when in.ConfigFiles is
	// empty we compute the config list ourselves and pass it as the configs arg.
	configs := in.ConfigFiles
	if len(configs) == 0 {
		configs = resolveComposeFiles(in.ProjectDir, in.Env) // existing config files (incl. standard-name discovery when COMPOSE_FILE unset); empty only when the gate already skipped Layer-2
	}
	opts, err := cli.NewProjectOptions(configs,
		cli.WithWorkingDirectory(in.ProjectDir),
		cli.WithEnv(in.Env),              // FIRST: seeds o.Environment so the options below see it
		cli.WithConfigFileEnv,            // now sees COMPOSE_FILE if set; harmless when configs explicit
		cli.WithDefaultConfigPath,        // default docker-compose.y*ml / compose.y*ml discovery
		cli.WithProfiles(in.Profiles),    // M3 profiles passthrough
		cli.WithResolvedPaths(true),      // EnvFile.Path => absolute
		cli.WithInterpolation(true),      // ${...} in paths resolves from in.Env
		cli.WithoutEnvironmentResolution, // D1 lever: missing required env_file does not abort
	)
	if err != nil {
		return Result{}, fmt.Errorf("compose project options: %w", err)
	}
	proj, err := opts.LoadProject(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load compose project: %w", err)
	}

	view := ProjectView{WorkingDir: proj.WorkingDir, Services: map[string][]string{}}
	var out []string
	seen := map[string]bool{}

	// Deterministic emission: sort service names; preserve env_file order within a service.
	names := make([]string, 0, len(proj.Services))
	for name := range proj.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		svc := proj.Services[name]
		var existing []string
		for _, ef := range svc.EnvFiles {
			if _, statErr := os.Stat(ef.Path); statErr != nil {
				continue // D1 skip-missing parity (compose-go keeps the path; we drop it)
			}
			existing = append(existing, ef.Path)
			if !seen[ef.Path] {
				seen[ef.Path] = true
				out = append(out, ef.Path)
			}
		}
		view.Services[name] = existing
	}
	return Result{EnvFiles: out, Project: view}, nil
}
```

- [ ] **Step 5: Implement `internal/engine/discover.go`** (one shared COMPOSE_FILE resolver + the G4 chain-only gate)

ONE resolver, `resolveComposeFiles`, is used by BOTH `engine.Resolve` (Step 4, as the `configs` arg when `in.ConfigFiles` is empty) AND the `HasComposeFileEnv` gate, so the gate and the loader can never disagree. It (a) reads `COMPOSE_FILE` from the seed env; (b) interpolates `${COMPOSE_ENV}`/`${ENV}` (and any var in the seed env); (c) splits on `COMPOSE_PATH_SEPARATOR` if set in the seed env, else `os.PathListSeparator` (`:` on unix) — **never `,`** (compose-go never uses `,`; probe-verified v2.11.0); (d) joins relative entries to absolute against `dir`; (e) keeps only entries that exist on disk; (f) **when `COMPOSE_FILE` is unset, falls back to standard-name discovery in `dir`** so it is a complete answer. `HasComposeFileEnv(dir, env)` is `len(resolveComposeFiles(...)) > 0`; the convenience `HasComposeFile(dir, composeFileEnv)` wraps it.

```go
package engine

import (
	"os"
	"path/filepath"
	"strings"
)

// standardComposeNames matches compose-go's default discovery set.
var standardComposeNames = []string{
	"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml",
}

func seedLookup(env []string, key string) string {
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			return kv[len(key)+1:]
		}
	}
	return ""
}

// interpolateComposeFile substitutes ${COMPOSE_ENV}/${ENV} (and any var present
// in the seed env) into a COMPOSE_FILE entry. This mirrors the semantics
// chain.substituteTokens applies in Layer-1 (replicated here because that helper
// is unexported in package chain), so the gate and the loader interpolate alike.
func interpolateComposeFile(entry string, env []string) string {
	composeEnv := seedLookup(env, "COMPOSE_ENV")
	r := strings.NewReplacer("${COMPOSE_ENV}", composeEnv, "${ENV}", composeEnv)
	return r.Replace(entry)
}

// resolveComposeFiles turns the seed env into an ordered slice of existing
// absolute config paths — the single resolver shared by Resolve (the configs
// arg) and the HasComposeFileEnv gate, so they cannot drift. When COMPOSE_FILE
// is UNSET it falls back to standard-name discovery in dir (so the resolver is
// itself a complete "what config files exist?" answer; qa's discover_test pins
// len(resolveComposeFiles(dir, env)) > 0 for a bare compose.yaml with no
// COMPOSE_FILE set).
func resolveComposeFiles(dir string, env []string) []string {
	cf := seedLookup(env, "COMPOSE_FILE")
	if cf == "" {
		var out []string
		for _, n := range standardComposeNames {
			p := filepath.Join(dir, n)
			if _, err := os.Stat(p); err == nil {
				out = append(out, p)
			}
		}
		return out
	}
	sep := seedLookup(env, "COMPOSE_PATH_SEPARATOR")
	if sep == "" {
		sep = string(os.PathListSeparator) // ":" on unix — NEVER ","
	}
	var out []string
	for _, raw := range strings.Split(cf, sep) {
		f := strings.TrimSpace(interpolateComposeFile(raw, env))
		if f == "" {
			continue
		}
		if !filepath.IsAbs(f) {
			f = filepath.Join(dir, f) // resolve against ProjectDir, NOT process cwd
		}
		if _, err := os.Stat(f); err == nil {
			out = append(out, f)
		}
	}
	return out
}

// HasComposeFileEnv is the seam-correct gate: it takes the FULL seed env (the
// chain's cr.Vars) so ${COMPOSE_ENV} interpolation and COMPOSE_PATH_SEPARATOR are
// honored, and shares resolveComposeFiles with Resolve so gate and loader cannot
// drift. When false, callers skip Layer-2 entirely (chain-only mode, spec §13 G4).
func HasComposeFileEnv(dir string, env []string) bool {
	return len(resolveComposeFiles(dir, env)) > 0
}

// HasComposeFile is the single-COMPOSE_FILE-value convenience form (still
// interpolates ${COMPOSE_ENV} when the value carries it AND the caller seeds
// COMPOSE_ENV — prefer HasComposeFileEnv from cmd code, which threads the full env).
func HasComposeFile(dir, composeFileEnv string) bool {
	if composeFileEnv != "" {
		return HasComposeFileEnv(dir, []string{"COMPOSE_FILE=" + composeFileEnv})
	}
	return HasComposeFileEnv(dir, nil)
}
```

- [ ] **Step 6: Run the D1 test — verify it PASSES**

Run: `go test ./internal/engine/ -run MissingRequired -v`
Expected: PASS (enumeration lenient; MISSING.env filtered).

- [ ] **Step 7: Add determinism + dedup + cross-subproject + profile tests against `examples/monorepo/`**

`internal/engine/engine_test.go` (append):
```go
// Determinism: same inputs => byte-identical ordering across runs.
func TestResolve_Deterministic(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "compose.yaml", `
services:
  zeta: { image: busybox, env_file: [./z.env] }
  alpha: { image: busybox, env_file: [./a.env] }
`)
	write(t, dir, "z.env", "Z=1\n")
	write(t, dir, "a.env", "A=1\n")
	var first []string
	for i := 0; i < 5; i++ {
		res, err := engine.New().Resolve(context.Background(), engine.Input{ProjectDir: dir})
		if err != nil {
			t.Fatal(err)
		}
		bn := make([]string, len(res.EnvFiles))
		for j, p := range res.EnvFiles {
			bn[j] = filepath.Base(p)
		}
		if i == 0 {
			first = bn
			// sorted by service name: alpha(a.env) before zeta(z.env)
			if first[0] != "a.env" || first[1] != "z.env" {
				t.Fatalf("not sorted by service: %v", first)
			}
		} else if first[0] != bn[0] || first[1] != bn[1] {
			t.Fatalf("non-deterministic: run0=%v run%d=%v", first, i, bn)
		}
	}
}

// Cross-subproject via include: (smoke-monorepo scenario 3/4/21).
func TestResolve_MonorepoFixture_CrossSubproject(t *testing.T) {
	root, err := filepath.Abs("../../examples/monorepo")
	if err != nil {
		t.Fatal(err)
	}
	res, err := engine.New().Resolve(context.Background(), engine.Input{
		ProjectDir: root,
		Env:        []string{"COMPOSE_ENV=dev"},
	})
	if err != nil {
		t.Fatalf("Resolve monorepo: %v", err)
	}
	got := map[string]bool{}
	for _, p := range res.EnvFiles {
		got[filepath.Base(p)] = true
	}
	for _, want := range []string{".web.env", ".api.env"} {
		if !got[want] {
			t.Fatalf("expected %s in EnvFiles; got %v", want, res.EnvFiles)
		}
	}
}
```
> Note: the exact expected basenames in `TestResolve_MonorepoFixture_CrossSubproject` MUST be reconciled with the real `examples/monorepo/` tree at implementation time (qa: open `examples/monorepo/web/` and `api/` to confirm filenames). Adjust the `want` slice to the actual per-service env file names.

- [ ] **Step 7b: COMPOSE_FILE interpolated-overlay RED test (docker-FREE; the load-bearing guard for findings [0]/[7]/[8]).** Use a SCRATCH `t.TempDir()` fixture (NOT `examples/monorepo` — its dev/prod overlays differ only by `environment: STACK_TIER`, which the engine never returns; the engine returns `EnvFiles`/`ProjectView.Services`). The overlay must add a service+env_file the base lacks, selectable ONLY via the interpolated `COMPOSE_FILE`. This test is RED against the pre-fix option order AND against delegating interpolation to `WithConfigFileEnv` (both probe-verified to drop the overlay), GREEN after Step 4/Step 5/Step 9.

`internal/engine/engine_test.go` (append):
```go
// COMPOSE_FILE=base.yml:overlay.${COMPOSE_ENV}.yml must select the env-specific
// overlay's env_file. Docker-free: asserts Result.EnvFiles, not a compose-config value.
func TestResolve_InterpolatedComposeFileOverlay(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "docker-compose.yml", `
services:
  web: { image: busybox, env_file: [./base.env] }
`)
	write(t, dir, "docker-compose.dev.yml", `
services:
  web: { image: busybox, env_file: [./dev-only.env] }
`)
	write(t, dir, "docker-compose.prod.yml", `
services:
  web: { image: busybox, env_file: [./prod-only.env] }
`)
	write(t, dir, "base.env", "BASE=1\n")
	write(t, dir, "dev-only.env", "D=1\n")
	write(t, dir, "prod-only.env", "P=1\n")

	load := func(env string) map[string]bool {
		res, err := engine.New().Resolve(context.Background(), engine.Input{
			ProjectDir: dir,
			Env: []string{
				"COMPOSE_ENV=" + env,
				"COMPOSE_FILE=docker-compose.yml:docker-compose.${COMPOSE_ENV}.yml",
			},
		})
		if err != nil {
			t.Fatalf("Resolve(%s): %v", env, err)
		}
		got := map[string]bool{}
		for _, p := range res.EnvFiles {
			got[filepath.Base(p)] = true
		}
		return got
	}
	prod := load("prod")
	if !prod["prod-only.env"] || prod["dev-only.env"] {
		t.Fatalf("prod overlay wrong: %v (want prod-only.env present, dev-only.env absent)", prod)
	}
	dev := load("dev")
	if !dev["dev-only.env"] || dev["prod-only.env"] {
		t.Fatalf("dev overlay wrong: %v (want dev-only.env present, prod-only.env absent)", dev)
	}
}
```

- [ ] **Step 7c: `HasComposeFile` RED→GREEN table tests (new `internal/engine/discover_test.go`, qa-owned).** Closes the coverage gap on the Layer-2 gate (the file-structure table now marks `discover.go` test=yes). RED first: against the pre-fix discover.go these fail (the `,` heuristic and raw-stat-without-interpolation cases); GREEN after Step 5.

`internal/engine/discover_test.go`:
```go
package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func touch(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHasComposeFile(t *testing.T) {
	tests := []struct {
		name    string
		present []string // files to create in the temp dir
		cfEnv   string   // COMPOSE_FILE value ("" = unset → standard-name discovery)
		want    bool
	}{
		{"unset + standard name present", []string{"compose.yaml"}, "", true},
		{"unset + no standard name", nil, "", false},
		{"explicit a.yml present", []string{"a.yml"}, "a.yml", true},
		{"explicit a.yml missing", nil, "a.yml", false},
		{"colon list, only first exists", []string{"a.yml"}, "a.yml:b.yml", true},
		{"empty value falls to discovery (none) => false", nil, "", false},
		// token-only entry must interpolate before stat (RED on raw-stat impl):
		{"interpolated-only overlay exists", []string{"docker-compose.prod.yml"},
			"docker-compose.${COMPOSE_ENV}.yml", true},
		// comma must NOT be treated as a separator (RED on the deleted heuristic):
		{"comma-joined is a single (nonexistent) path", []string{"a.yml", "b.yml"},
			"a.yml,b.yml", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tc.present {
				touch(t, dir, f)
			}
			// Pass the full seed env so ${COMPOSE_ENV} interpolates.
			env := []string{"COMPOSE_ENV=prod"}
			if tc.cfEnv != "" {
				env = append(env, "COMPOSE_FILE="+tc.cfEnv)
			}
			got := len(resolveComposeFiles(dir, env)) > 0
			if got != tc.want {
				t.Fatalf("resolveComposeFiles present=%v: got %v want %v", tc.present, got, tc.want)
			}
		})
	}
}
```
> Temp-revert validity (qa, before accepting): the comma case must be RED if the deleted `,` heuristic is restored, and the interpolated-overlay case must be RED if interpolation is removed before the stat. Confirm both flip, then keep the corrected resolver.

- [ ] **Step 8: Lock the engine seam — assert no other package imports compose-go**

`internal/engine/engine_test.go` (append):
```go
// (placed in a separate file is fine) — but easiest as a Makefile/CI check:
```
Run (CI gate, also runnable locally):
```bash
# Only internal/engine may import compose-go. NO -deps: the left column must be
# our OWN packages only (with -deps, compose-go's ~17 sub-packages appear in the
# left column and the gate is a permanent false positive — RED on every clean
# build). Anchor the engine filter as "^$MOD/internal/engine " (start-of-line +
# trailing space) so only the engine package's own ImportPath line is stripped.
MOD=$(go list -m)
go list -f '{{.ImportPath}} {{join .Imports " "}}' ./... \
  | grep -v "^$MOD/internal/engine " \
  | grep 'compose-spec/compose-go' \
  && { echo "compose-go leaked outside internal/engine"; exit 1; } \
  || echo "seam OK"
```
Expected: `seam OK`.

> Guard-validity check (temp-revert; go-engineer/qa must do this before "done"): on a clean tree the gate prints `seam OK`; temporarily add `import _ "github.com/compose-spec/compose-go/v2/types"` to a `chain` package file → the gate must print `compose-go leaked…` + exit 1; revert. Only a gate that flips between those two states is valid. (The CI yaml author in Task 8 Step 3 must paste THIS corrected form, not the original.)

- [ ] **Step 9: COMPOSE_FILE overlay — manual interpolate+split is the PRIMARY path (not a fallback)**

This is already wired in Step 4 + Step 5: `resolveComposeFiles` is the mandatory handler for `COMPOSE_FILE` whenever `in.ConfigFiles` is empty. There is **no "no code change" branch** — it is unreachable. Probe-verified against compose-go v2.11.0 (cli/options.go + `.claude/artifacts/compose-go-d1-lever.md`): with the original config-first option order a seed `COMPOSE_FILE` is silently dropped (the load env is empty when `WithConfigFileEnv` runs); and even with the order fixed, `WithConfigFileEnv`/`absolutePaths` does **not** interpolate `${VAR}` inside `COMPOSE_FILE` and resolves relative entries via `filepath.Abs` against the **process cwd**, not `WithWorkingDirectory(in.ProjectDir)` — so the fixture value `docker-compose.yml:docker-compose.${COMPOSE_ENV}.yml` (`examples/monorepo/example.env:18`) would be stat'd literally and dropped, and a literal relative entry would miss when cwd ≠ ProjectDir. The manual path (interpolate from `in.Env`, split on `COMPOSE_PATH_SEPARATOR`-else-`os.PathListSeparator`, join-to-abs against `in.ProjectDir`, pass as the `configs` arg) is the only one of the three probed approaches that loads base+overlay.

Validity guard (do NOT use scenario 15 — it execs real `docker compose`, which interpolates `COMPOSE_FILE` from its own auto-loaded `.env`, so it stays green regardless of this bug). The load-bearing guard is the **docker-free engine RED test in Step 7** (scratch overlay carrying a unique env_file). Record the probe outcome in a one-line comment above `resolveComposeFiles` and in `.claude/agent-memory/go-engineer/`.

- [ ] **Step 10: Run the full engine suite + seam check, vet, fmt**

Run: `go test ./internal/engine/ -v && go vet ./internal/engine/ && gofmt -l internal/engine/`
Expected: PASS; seam OK; no vet/fmt output.

- [ ] **Step 11: Commit**

```bash
git add go.mod go.sum internal/engine/engine.go internal/engine/discover.go internal/engine/engine_test.go
git commit -m "feat(engine): Layer-2 enumeration via compose-go v2.11.0 (D1 lever, determinism)"
```

---

## Task 4: `internal/envfiles` — merge Layer-1 + Layer-2

**Owner:** go-engineer (impl) + qa-engineer (tests). **Depends on:** T2, T3.

**Responsibility:** Combine `chain.Result.Files` (Layer-1) and `engine.Result.EnvFiles` (Layer-2) into one ordered, deduped slice for `COMPOSE_ENV_FILES` (Layer-1 first in chain order with secrets last; Layer-2 after; a path in both is emitted once in its Layer-1 position — audit W3), and guard the `,` separator.

**Files:**
- Create: `internal/envfiles/assemble.go`
- Test: `internal/envfiles/assemble_test.go`

- [ ] **Step 1: Write the failing W3 test (secrets stay last within Layer-1; dedup keeps Layer-1 position)**

`internal/envfiles/assemble_test.go`:
```go
package envfiles

import "testing"

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestAssemble_OrderDedupSecretsLast(t *testing.T) {
	layer1 := []string{"/p/.env", "/p/.dev.env", "/p/.secrets.env"}
	layer2 := []string{"/p/web/.web.env", "/p/.dev.env" /* dup of layer1 */, "/p/api/.api.env"}
	got, err := Assemble(layer1, layer2)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"/p/.env", "/p/.dev.env", "/p/.secrets.env", // Layer-1 untouched, secrets last
		"/p/web/.web.env", "/p/api/.api.env", // Layer-2, dup dropped, order preserved
	}
	if !eq(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestAssemble_RejectsComma(t *testing.T) {
	if _, err := Assemble([]string{"/p/a,b.env"}, nil); err == nil {
		t.Fatal("expected error on comma in path")
	}
}
```

- [ ] **Step 2: Run — verify FAIL** (`undefined: Assemble`)

Run: `go test ./internal/envfiles/ -v`
Expected: FAIL (build error).

- [ ] **Step 3: Implement `internal/envfiles/assemble.go`**

```go
// Package envfiles merges the Layer-1 chain and the Layer-2 enumerated set into
// the final ordered COMPOSE_ENV_FILES list. Pure Go.
package envfiles

import (
	"fmt"
	"strings"
)

// Assemble returns Layer-1 (in chain order, secrets last by construction) followed
// by Layer-2, with any path present in both emitted once in its Layer-1 position.
// Variable precedence is last-wins by this order at docker-compose load time.
func Assemble(layer1, layer2 []string) ([]string, error) {
	out := make([]string, 0, len(layer1)+len(layer2))
	seen := map[string]bool{}
	add := func(p string) error {
		if strings.ContainsRune(p, ',') {
			return fmt.Errorf("env file path %q contains a comma (COMPOSE_ENV_FILES separator)", p)
		}
		if seen[p] {
			return nil
		}
		seen[p] = true
		out = append(out, p)
		return nil
	}
	for _, p := range layer1 {
		if err := add(p); err != nil {
			return nil, err
		}
	}
	for _, p := range layer2 {
		if err := add(p); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Join renders the assembled list as a COMPOSE_ENV_FILES value.
func Join(files []string) string { return strings.Join(files, ",") }
```

- [ ] **Step 4: Run — verify PASS**

Run: `go test ./internal/envfiles/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/envfiles/assemble.go internal/envfiles/assemble_test.go
git commit -m "feat(envfiles): ordered+deduped COMPOSE_ENV_FILES assembly (W3 + comma guard)"
```

---

## Task 5: `internal/bootstrap` — `cenvkit init`

**Owner:** go-engineer (impl) + qa-engineer (tests). **Depends on:** T1 (parallel with T2/T3).

**Responsibility:** Seed `.<X>` from `example.<X>` **no-clobber** in the project dir and fan out one level into immediate subdirectories. No `sudo`, no `chmod 777`, never overwrite an existing file (W5 secret-wipe guard).

**Files:**
- Create: `internal/bootstrap/bootstrap.go`
- Test: `internal/bootstrap/bootstrap_test.go`

- [ ] **Step 1: Write the failing W5 no-clobber test (RED on a clobbering impl)**

`internal/bootstrap/bootstrap_test.go`:
```go
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
```

- [ ] **Step 2: Run — verify FAIL** (`undefined: Init`)

Run: `go test ./internal/bootstrap/ -v`
Expected: FAIL.

- [ ] **Step 3: Implement `internal/bootstrap/bootstrap.go`**

```go
// Package bootstrap implements `cenvkit init`: seed .<X> from example.<X>
// no-clobber, fanning out one directory level. No sudo, no chmod 777, never
// overwrites an existing file (secret-wipe guard).
package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Init seeds the given directory and its immediate subdirectories.
func Init(dir string) error {
	if err := seedDir(dir); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if e.Type()&os.ModeSymlink != 0 {
			continue // skip symlinks (parity with init.sh)
		}
		if err := seedDir(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// seedDir copies each example.<X> to .<X> when the target does not already exist.
func seedDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "example") {
			continue
		}
		target := strings.TrimPrefix(e.Name(), "example") // example.env -> .env
		if target == "" || target == e.Name() {
			continue
		}
		dst := filepath.Join(dir, target)
		if _, err := os.Stat(dst); err == nil {
			continue // no-clobber
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat %s: %w", dst, err)
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dst, err)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run — verify PASS**

Run: `go test ./internal/bootstrap/ -v`
Expected: PASS (both W5 no-clobber and seed/fan-out/idempotent).

- [ ] **Step 5: Commit**

```bash
git add internal/bootstrap/bootstrap.go internal/bootstrap/bootstrap_test.go
git commit -m "feat(bootstrap): cenvkit init seed+fanout, no-clobber secret guard (W5)"
```

---

## Task 6: `internal/debug` + `cmd/cenvkit` wiring

**Owner:** go-engineer (impl) + qa-engineer (tests). **Depends on:** T2, T3, T4, T5.

**Responsibility:** (a) env-debug modes over the merged file set + a tiny dotenv merge (compose-go-free); (b) wire all subcommands: `env-files`, `compose`, `env-debug`, `validate`, `init`, `version`; assemble `COMPOSE_ENV_FILES` and `exec docker compose`; accept-but-ignore `COMPOSE_DEPTH`.

**Files:**
- Create: `internal/debug/debug.go`, `internal/debug/debug_test.go`
- Modify: `cmd/cenvkit/main.go`

### 6A — `internal/debug`

**v1 `--value`/`--trace` contract (Option B — thin, decided 2026-06-15).** v1
`--value` and `--trace` return **RAW last-wins literals** with **NO**
`${VAR:-default}` / `${VAR:+x}` / cross-ref expansion — a deliberate narrowing
from the sh kit, where `--value` shell-sourced the chain and expanded defaults.
This is documented in the Task 9 migration docs; users who need fully-resolved
values use `--effective` (= `docker compose config`). Given `FOO=${MISSING:-fallback}`,
v1 returns the literal string `"${MISSING:-fallback}"`, not `"fallback"`.

- [ ] **Step 1: Write the failing `--value`/`--trace` tests** (smoke.sh 5.6/5.7 at unit level + the last-wins and Option-B guards)

`internal/debug/debug_test.go`:
```go
package debug

import (
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
```
> Temp-revert validity (qa): flip `mergeDotEnv`'s `vals[k] = v` (last wins) to a first-wins variant and confirm `TestValue_LastWins` goes RED; restore. The production code (line shown in Step 3) is already last-wins — only the tests change.

- [ ] **Step 1b: `--value` scope + `--trace` cross-layer guards (finding-driven).** `--value` is sourced from the **Layer-1 chain only** (legacy `lib/env-debug.sh:111-117,133`; `smoke.sh:218` "--value sources ONLY the project chain"), so the cmd wiring (Step 5) feeds `cr.Files` to `debug.Value`. `--trace` stays on the **merged** list (legacy trace is Layer-2-rooted: `smoke.sh:213` traces `SVC_PORT`, defined only in the Layer-2 `svc.env`). These tests lock that contract.

`internal/debug/debug_test.go` (append):
```go
import "bytes"

// --value scope: Value reads Layer-1 only. A var set in BOTH layers must return
// the Layer-1 value when given the Layer-1 list, and the Layer-2 value when given
// the merged list (demonstrating the divergence the cmd wiring removes by feeding
// cr.Files to Value). RED if the wiring passed merged to Value.
func TestValue_ScopedToLayer1(t *testing.T) {
	dir := t.TempDir()
	l1 := filepath.Join(dir, ".env")            // Layer-1
	l2 := filepath.Join(dir, "web", "svc.env")  // Layer-2
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
```

- [ ] **Step 2: Run — verify FAIL**

Run: `go test ./internal/debug/ -v`
Expected: FAIL (`undefined: Value`).

- [ ] **Step 3: Implement `internal/debug/debug.go`**

```go
// Package debug renders env-debug views over the assembled file set and a tiny
// dotenv merge. Pure Go — no compose-go (consumes engine.ProjectView only).
package debug

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

func mergeDotEnv(files []string) (map[string]string, map[string][]string) {
	vals := map[string]string{}
	sources := map[string][]string{} // var -> files that set it (in order)
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			i := strings.IndexByte(line, '=')
			if i <= 0 {
				continue
			}
			k := strings.TrimSpace(line[:i])
			v := strings.TrimSpace(line[i+1:])
			vals[k] = v // last wins
			sources[k] = append(sources[k], f)
		}
		fh.Close()
	}
	return vals, sources
}

// Value returns the effective (last-wins) value of name across files; "" if unset.
func Value(files []string, name string) string {
	vals, _ := mergeDotEnv(files)
	return vals[name]
}

// PrintChain writes the Layer-1 file list, one per line.
func PrintChain(w io.Writer, layer1 []string) {
	for _, f := range layer1 {
		fmt.Fprintln(w, f)
	}
}

// PrintFiles writes the full assembled COMPOSE_ENV_FILES list, one per line.
func PrintFiles(w io.Writer, all []string) {
	for _, f := range all {
		fmt.Fprintln(w, f)
	}
}

// Trace shows which files set name and the effective value.
func Trace(w io.Writer, files []string, name string) {
	vals, sources := mergeDotEnv(files)
	for _, src := range sources[name] {
		fmt.Fprintf(w, "%s\t%s\n", name, src)
	}
	fmt.Fprintf(w, "effective %s=%s\n", name, vals[name])
}

// Diff lists variables contributed by Layer-2 that are not present in Layer-1.
func Diff(w io.Writer, layer1, layer2 []string) {
	l1, _ := mergeDotEnv(layer1)
	l2, _ := mergeDotEnv(layer2)
	for k, v := range l2 {
		if _, inL1 := l1[k]; !inL1 {
			fmt.Fprintf(w, "+ %s=%s\n", k, v)
		}
	}
}
```

- [ ] **Step 4: Run — verify PASS**

Run: `go test ./internal/debug/ -v`
Expected: PASS.

### 6B — `cmd/cenvkit` wiring

- [ ] **Step 5: Add a resolve helper + subcommands to `cmd/cenvkit/main.go`**

Add these imports: `context`, `os/exec`, `path/filepath`, `strings`, and the four internal packages (`internal/chain`, `internal/engine`, `internal/envfiles`, `internal/debug`, `internal/bootstrap`).

```go
// resolveProjectDir honors --project-dir, defaulting to the current directory.
func resolveProjectDir(cmd *cobra.Command) (string, error) {
	pd, _ := cmd.Flags().GetString("project-dir")
	if pd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		pd = wd
	}
	return filepath.Abs(pd)
}

// assemble runs Layer-1 + Layer-2 and returns the merged COMPOSE_ENV_FILES list,
// the chain result, and the engine result (engine empty when no compose file).
// envOverlay entries (e.g. "COMPOSE_ENV=prod") are appended AFTER os.Environ() so
// they win via chain's osEnvMap last-wins — this is how `validate --all` re-resolves
// the Layer-1 chain per env (findings [3]/[9]).
func assemble(cmd *cobra.Command, envOverlay ...string) ([]string, chain.Result, engine.Result, error) {
	dir, err := resolveProjectDir(cmd)
	if err != nil {
		return nil, chain.Result{}, engine.Result{}, err
	}
	osEnv := append(os.Environ(), envOverlay...)
	cr, err := chain.Resolve(chain.Input{ProjectDir: dir, OSEnv: osEnv, Hostname: os.Hostname})
	if err != nil {
		return nil, chain.Result{}, engine.Result{}, err
	}
	var er engine.Result
	// HasComposeFileEnv takes the FULL seed env (cr.Vars) so ${COMPOSE_ENV} in a
	// COMPOSE_FILE entry interpolates and COMPOSE_PATH_SEPARATOR is honored — it
	// shares the resolveComposeFiles seam with engine.Resolve, so gate and loader
	// cannot drift (findings [10]/[22]/[23]). cr.Vars carries COMPOSE_ENV.
	if engine.HasComposeFileEnv(dir, cr.Vars) {
		er, err = engine.New().Resolve(context.Background(), engine.Input{
			ProjectDir: dir,
			Env:        cr.Vars,
			Profiles:   splitProfiles(envValue(cr.Vars, "COMPOSE_PROFILES")),
		})
		if err != nil {
			return nil, chain.Result{}, engine.Result{}, err
		}
	}
	merged, err := envfiles.Assemble(cr.Files, er.EnvFiles)
	if err != nil {
		return nil, chain.Result{}, engine.Result{}, err
	}
	return merged, cr, er, nil
}

func envValue(vars []string, key string) string {
	for _, kv := range vars {
		if strings.HasPrefix(kv, key+"=") {
			return kv[len(key)+1:]
		}
	}
	return ""
}

func splitProfiles(v string) []string {
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}

func newEnvFilesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "env-files",
		Short: "Print the resolved COMPOSE_ENV_FILES chain, one path per line",
		RunE: func(cmd *cobra.Command, _ []string) error {
			merged, _, _, err := assemble(cmd)
			if err != nil {
				return err
			}
			for _, f := range merged {
				fmt.Fprintln(cmd.OutOrStdout(), f)
			}
			return nil
		},
	}
}

func newComposeCmd() *cobra.Command {
	c := &cobra.Command{
		Use:                "compose [args...]",
		Short:              "Assemble the chain and exec `docker compose`",
		DisableFlagParsing: true, // pass everything through to docker compose
		RunE: func(cmd *cobra.Command, args []string) error {
			// DisableFlagParsing means cobra does NOT parse the persistent
			// --project-dir here; it leaks into args and `docker compose` would
			// reject it as an unknown flag. Pre-scan args: extract its value to
			// override the project dir, and STRIP every occurrence (both
			// `--project-dir VAL` and `--project-dir=VAL`, in any position) so it
			// is never forwarded to docker compose. (finding [5])
			dirOverride, args := extractProjectDir(args)
			if dirOverride != "" {
				_ = cmd.Flags().Set("project-dir", dirOverride)
			}
			merged, _, _, err := assemble(cmd)
			if err != nil {
				return err
			}
			dir, err := resolveProjectDir(cmd)
			if err != nil {
				return err
			}
			dc := exec.Command("docker", append([]string{"compose"}, args...)...)
			dc.Dir = dir // run where the chain/engine resolved files (lib/compose-env.sh:130-131)
			dc.Env = append(os.Environ(), "COMPOSE_ENV_FILES="+envfiles.Join(merged))
			dc.Stdin, dc.Stdout, dc.Stderr = os.Stdin, os.Stdout, os.Stderr
			if err := dc.Run(); err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					os.Exit(ee.ExitCode())
				}
				return fmt.Errorf("exec docker compose: %w", err)
			}
			return nil
		},
	}
	return c
}

// extractProjectDir pulls --project-dir out of a DisableFlagParsing arg slice,
// supporting `--project-dir VAL` and `--project-dir=VAL` in any position, and
// returns the value (last wins) plus args with all occurrences removed.
func extractProjectDir(args []string) (string, []string) {
	val := ""
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--project-dir":
			if i+1 < len(args) {
				val = args[i+1]
				i++ // skip the value token
			}
		case strings.HasPrefix(a, "--project-dir="):
			val = strings.TrimPrefix(a, "--project-dir=")
		default:
			out = append(out, a)
		}
	}
	return val, out
}

func newValidateCmd() *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use:   "validate",
		Short: "Validate the resolved compose config (docker compose config -q)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			run := func(env string) error {
				var ov []string
				if env != "" {
					ov = []string{"COMPOSE_ENV=" + env} // re-resolve the Layer-1 chain for THIS env
				}
				merged, _, _, err := assemble(cmd, ov...)
				if err != nil {
					return err
				}
				dir, err := resolveProjectDir(cmd)
				if err != nil {
					return err
				}
				dc := exec.Command("docker", "compose", "config", "-q")
				dc.Dir = dir
				dc.Env = append(os.Environ(), "COMPOSE_ENV_FILES="+envfiles.Join(merged))
				if env != "" {
					dc.Env = append(dc.Env, "COMPOSE_ENV="+env) // also render ${COMPOSE_ENV} in compose files
				}
				dc.Stdout, dc.Stderr = os.Stdout, os.Stderr
				return dc.Run()
			}
			if all {
				if err := run("dev"); err != nil {
					return fmt.Errorf("dev config invalid: %w", err)
				}
				return run("prod")
			}
			return run("")
		},
	}
	c.Flags().BoolVar(&all, "all", false, "validate both dev and prod")
	return c
}

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Seed .X from example.X (no-clobber) and fan out one level",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := resolveProjectDir(cmd)
			if err != nil {
				return err
			}
			return bootstrap.Init(dir)
		},
	}
}

func newEnvDebugCmd() *cobra.Command {
	var (
		mChain, mDiff, mEffective, mFiles, mTrace, mValue bool
		varName                                           string
	)
	c := &cobra.Command{
		Use:   "env-debug",
		Short: "Inspect the resolved env chain",
		RunE: func(cmd *cobra.Command, _ []string) error {
			merged, cr, _, err := assemble(cmd)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			switch {
			case mValue:
				fmt.Fprintln(out, debug.Value(cr.Files, varName)) // Layer-1 chain only (smoke.sh:218)
			case mTrace:
				debug.Trace(out, merged, varName) // merged: --trace must see Layer-2 (e.g. SVC_PORT)
			case mFiles:
				debug.PrintFiles(out, merged)
			case mDiff:
				debug.Diff(out, cr.Files, sliceAfter(merged, cr.Files))
			case mEffective:
				dc := exec.Command("docker", "compose", "config")
				dc.Env = append(os.Environ(), "COMPOSE_ENV_FILES="+envfiles.Join(merged))
				dc.Stdout, dc.Stderr = out, os.Stderr
				return dc.Run()
			default: // --chain is the default view
				_ = mChain
				debug.PrintChain(out, cr.Files)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&mChain, "chain", false, "show Layer-1 chain files")
	c.Flags().BoolVar(&mDiff, "diff", false, "show vars Layer-2 adds over Layer-1")
	c.Flags().BoolVar(&mEffective, "effective", false, "docker compose config (rendered)")
	c.Flags().BoolVar(&mFiles, "files", false, "show full COMPOSE_ENV_FILES list")
	c.Flags().BoolVar(&mTrace, "trace", false, "trace files that set --var")
	c.Flags().BoolVar(&mValue, "value", false, "print the effective value of --var")
	c.Flags().StringVar(&varName, "var", "", "variable name for --trace/--value")
	return c
}

// sliceAfter returns merged entries that are not in layer1 (the Layer-2 tail).
func sliceAfter(merged, layer1 []string) []string {
	in := map[string]bool{}
	for _, f := range layer1 {
		in[f] = true
	}
	var out []string
	for _, f := range merged {
		if !in[f] {
			out = append(out, f)
		}
	}
	return out
}
```

Register them in `newRootCmd()` (and note `COMPOSE_DEPTH` is read-but-ignored — no flag, no error):
```go
	root.AddCommand(newEnvFilesCmd(), newComposeCmd(), newValidateCmd(),
		newInitCmd(), newEnvDebugCmd())
	// COMPOSE_DEPTH is accepted-but-ignored (spec §5): include-graph load makes it obsolete.
```

- [ ] **Step 6: Build, vet, fmt, smoke-run against the fixture**

Run:
```bash
go build ./... && go vet ./... && gofmt -l .
( cd examples/monorepo && go run ../../cmd/cenvkit env-files )
```
Expected: clean build/vet/fmt; `env-files` prints the root `.env` plus the web/api env files (absolute paths).

- [ ] **Step 7: Commit**

```bash
git add internal/debug/debug.go internal/debug/debug_test.go cmd/cenvkit/main.go
git commit -m "feat(cmd): wire env-files/compose/env-debug/validate/init; COMPOSE_DEPTH ignored"
```

---

## Task 7: Acceptance — port the smoke suites to drive `cenvkit`

**Owner:** qa-engineer. **Depends on:** T6.

**Responsibility:** Port `test/smoke-monorepo.sh` (23 scenarios, **60** assertions after the S4 recount — see Step 6; the verified baseline is 61, minus the dropped depth-knob assertion 11.2) and `test/smoke.sh` to drive `cenvkit`, keeping assertion logic identical EXCEPT the deliberate upstream-driven inversions G1–G5. The Go tool is "v1 done" when the ported suites are green. Use `.claude/artifacts/acceptance-port-plan.md` as the per-assertion map (invocation substitution table in §0).

**Files:**
- Create: `test/cenvkit-acceptance_test.go` (Go harness that builds the binary once and runs subcommands; gate docker-dependent assertions behind `SMOKE_SKIP_DOCKER`).
- Optionally also adapt the shell suites in place to call a built `cenvkit` (keep the legacy `./docker`-driven copies untouched as reference — do NOT modify `lib/`, `bin/docker`, or the legacy `scripts/`).

- [ ] **Step 1: Build the binary once in `TestMain`; expose a `runCenvkit(dir, env, args...)` helper.**

```go
package acceptance

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var cenvkitBin string

func TestMain(m *testing.M) {
	tmp, _ := os.MkdirTemp("", "cenvkit-bin")
	cenvkitBin = filepath.Join(tmp, "cenvkit")
	build := exec.Command("go", "build", "-o", cenvkitBin, "../cmd/cenvkit")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic("build cenvkit: " + err.Error())
	}
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

func runCenvkit(t *testing.T, dir string, env []string, args ...string) (string, error) {
	t.Helper()
	c := exec.Command(cenvkitBin, args...)
	c.Dir = dir
	c.Env = append(os.Environ(), env...)
	out, err := c.CombinedOutput()
	return string(out), err
}

func dockerAvailable() bool { return os.Getenv("SMOKE_SKIP_DOCKER") != "1" }
```

- [ ] **Step 1b: Add a `stageMonorepo(t)` fixture-staging helper.** The tracked `examples/monorepo/` has ONLY `example.*` templates + Layer-2 service dotfiles — NO `.env`/`.dev.env`/`.prod.env`/`.secrets.env`. Running `cenvkit` against it directly yields ZERO Layer-1 files, and running `cenvkit init` in place would dirty the version-controlled fixture. So every scenario derives its dir from a staged copy (mirrors `smoke-monorepo.sh:102` copy + `:119-121` seed). (finding [4])

```go
import "os"  // (already imported above; shown for clarity)

// stageMonorepo copies the fixture into a fresh temp dir and seeds Layer-1
// dotfiles (example.* -> .*). Returns the staged root. NOTE: STACK_TIER comes
// from the docker-compose.{dev,prod}.yml OVERLAY, not from this dotfile seed;
// the seed is what makes IS_DEV and the COMPOSE_ENV-selected .dev.env/.prod.env
// overlay resolution work.
func stageMonorepo(t *testing.T) string {
	t.Helper()
	src, _ := filepath.Abs("../examples/monorepo")
	dst := t.TempDir()
	if out, err := exec.Command("cp", "-R", src+"/.", dst).CombinedOutput(); err != nil {
		t.Fatalf("stage fixture: %v\n%s", err, out)
	}
	for _, p := range [][2]string{
		{"example.env", ".env"},
		{"example.dev.env", ".dev.env"},
		{"example.prod.env", ".prod.env"},
	} {
		b, err := os.ReadFile(filepath.Join(dst, p[0]))
		if err != nil {
			t.Fatalf("read %s: %v", p[0], err)
		}
		if err := os.WriteFile(filepath.Join(dst, p[1]), b, 0o644); err != nil {
			t.Fatalf("seed %s: %v", p[1], err)
		}
	}
	return dst
}
```
> The `cenvkit init`/G5 seeding assertion and the docker `compose config` scenarios (IS_DEV scenario 17, dev/prod overlays) MUST run against a staged root so Layer-1 vars are present and `init` never writes into the tracked fixture. (Equivalently, staging can run `cenvkit init` in the temp copy to also exercise init's seeding path.)

- [ ] **Step 2: Port the chain-only + engine (no-docker) scenarios** (acceptance-port-plan §5 Phases 1–2): 4, 6.1–6.2, 8.1, 12, 13, 16, 17.1–17.2, 18.1(env-files), 21.1, 23, plus smoke.sh 5.6–5.7. (Scenario **22 is NOT in this positive-membership list** — it is an over-discovery inversion handled in Step 3; see finding-driven reclassification.) Each derives its dir from `stageMonorepo(t)` and runs `cenvkit env-files`/`env-debug` to assert membership/exclusion/ordering. Example (scenario 6.2 — sibling isolation, the critical negative):

```go
func TestApiDir_DoesNotSeeWebEnv(t *testing.T) {
	root := stageMonorepo(t)
	apiDir := filepath.Join(root, "api")
	out, err := runCenvkit(t, apiDir, nil, "env-files")
	if err != nil {
		t.Fatalf("env-files: %v\n%s", err, out)
	}
	if strings.Contains(out, ".web.env") {
		t.Fatalf("api scope leaked web/.web.env:\n%s", out)
	}
	if !strings.Contains(out, ".api.env") {
		t.Fatalf("api scope missing .api.env:\n%s", out)
	}
}
```

- [ ] **Step 3: Encode the deliberate inversions (G1–G5) — assert the CORRECT behavior, label it as an inversion.**

- **G1/G2 (scenarios 9, 10, 22): over-discovery eliminated.** A stray `extra/docker-compose-extra.yml` NOT in `include:`/`COMPOSE_FILE` is **NOT** discovered. Assert `.extra.env` is absent. compose-go's standard discovery DOES match `compose.yaml` — assert canonical names are found when they are the project file. Add a comment: `// INVERSION (G1/G2): sh over-discovery quirk gone; include-graph is authoritative.`
  - **Scenario 10 reframe:** both 10.1 (`compose.yaml NOT discovered`) and 10.2 (`renamed weird/docker-compose.yml IS discovered`) are stray files NOT in the root `include:`, so under the include-graph BOTH are **NOT discovered**. Assert both as "stray-not-discovered" rather than the old glob polarity. Count stays 2 (count-neutral reframe).
  - **Scenario 22 (submodule shape) — RECLASSIFIED to this inversion class.** The legacy suite creates `vendored/` and `vendored2/` subprojects at test time that the root `docker-compose.yml` does NOT `include:` (verified: include is exactly `./web`, `./api`, `./services/reports/`). The sh kit finds them by find-by-glob; compose-go never enumerates a non-included subproject's env_file. So assert `.vend.env` and `.vend2.env` are **ABSENT** from `cenvkit env-files`. The `.git` gitlink (22.1) vs real `.git` directory (22.2) distinction is **moot** under the include-graph (no `.git` pruning exists) — keep two negative assertions (polarity flip, count-neutral). Comment: `// INVERSION (G1/G2 class): sh find-by-glob discovered non-included subdirs; include-graph does not. .git gitlink vs .git dir is moot under compose-go.`
- **G3 (scenario 11): `COMPOSE_DEPTH` accepted-but-ignored.** Assert `COMPOSE_DEPTH=4 cenvkit env-files` behaves identically to no `COMPOSE_DEPTH` (no error, same output); **drop 11.2** (`depth-4 found with COMPOSE_DEPTH=4`) — it is untestable: `a/b/c/docker-compose.yml` is never in the root `include:`, so `.deep.env` is never enumerated regardless of depth. Keep/reframe 11.1 as "out-of-include = not-found." (This is the **−1** in the S4 recount, Step 6.) First **grep the suites to confirm no assertion depends on depth *behavior*** (spec §5 task): `grep -n COMPOSE_DEPTH test/smoke*.sh`.
- **G4 (scenario 14): no fallback shim.** Rewrite as "a project dir with no compose file resolves chain-only" — `cenvkit env-files` succeeds and lists only Layer-1. Assert it does NOT error (relies on `engine.HasComposeFileEnv` → skip Layer-2).
- **G5 (smoke.sh [2], scenario 1/7/20 install layout):** the Go layout has no `scripts/*.sh`/`*.mk`. Assert the Go install artifact set: the `cenvkit` binary (or vendored shim) + `.docker-env-chain` (back-compat). `cenvkit init` replaces `install.sh`/`init.sh` generation — assert seeding behavior, not an emitted `init.sh`.

> **Process guard (broaden the fix, finding [13]).** Before porting, audit EVERY scenario that `mkdir`s a subproject at test time against the fixture's `include:` block (exactly `./web`, `./api`, `./services/reports/` — `examples/monorepo/docker-compose.yml:20-23`). Any test-time subproject outside that set MUST invert to "not discovered" (done for 9, 22; verify 11 under G3). Assert the include graph, never the glob list.

- [ ] **Step 3b: C1 guard — `env_file:` path referencing a Layer-2-only var is unsupported (RED on a naive single-pass mis-resolve; spec §4a, audit C1).** Use a tiny THROWAWAY fixture (NOT in `examples/monorepo`, to keep the count stable — create under `t.TempDir()`): a compose file with two services. Service A's `env_file` (e.g. `a.env`) defines `ONLY_IN_A=sub`. Service B declares `env_file: ${ONLY_IN_A}/.b.env` — a path depending on a var defined ONLY inside A's Layer-2 env_file (NOT in any Layer-1 `.env`/`.docker-env-chain`). Run `cenvkit env-files` and assert the path does NOT silently resolve: compose-go (seeded only with Layer-1 via `WithEnv`) leaves `${ONLY_IN_A}` unresolved/empty, so the path is absent from the output (and per D1 `os.Stat`-filtering, a non-existent path is dropped) — assert `!strings.Contains(out, "/sub/.b.env")` AND no entry mis-resolves `ONLY_IN_A`. Guard validity: confirm this would FAIL (RED) against a hypothetical two-pass/fixpoint impl that fed Layer-2 vars back into path interpolation — it pins the single-pass §4a contract. This is a chain-only/no-docker assertion (env-files), no `dockerAvailable()` gate. It uses a SEPARATE throwaway fixture so it does NOT perturb the smoke-monorepo count (Step 6). Reference spec §4a + audit C1 (`.claude/artifacts/spec-audit.md`).

- [ ] **Step 3c: D1 runtime-fatal half — missing *required* env_file is fatal at the real `docker compose` run** (spec §4b; the assembly-skips half is unit-tested in Task 3 Step 2). Docker-gated (`if !dockerAvailable() { t.Skip() }`): stage a project where a service declares `env_file: [{path: ./MISSING.env, required: true}]` with `MISSING.env` absent, then run `cenvkit compose config` and assert a **non-zero exit** (the unmodified compose model, loaded WITHOUT the enumeration lever, re-enforces `required:`). This pins that cenvkit's `compose`/`validate` path does NOT carry the lever into the real run. Separate throwaway fixture; does not perturb the count.

- [ ] **Step 4: Port the contract-seam test** (acceptance-port-plan §3): feed `chain.Resolve()` output directly into `engine.Resolve()` and assert the merged `COMPOSE_ENV_FILES` ordering end-to-end. **Pin location: `test/seam_test.go`** (owner: qa-engineer), in `package acceptance` (same package as `cenvkit-acceptance_test.go`). It is a white-box Go test that imports `internal/chain` and `internal/engine` directly and calls `chain.Resolve()` → `engine.Resolve()` (not the binary-driven harness). Do NOT place it in `internal/engine` — that would put a qa-owned `*_test.go` inside go-engineer's package dir (violates the module boundaries and risks colliding with the reserved `internal/engine/engine_test.go`).

- [ ] **Step 5: Port the docker-dependent scenarios** (Phase 4): 3, 5.2, 6.3, 7.4, 8.2, 15, 17.3–17.4, 18.2–18.3, 19, 21.2 + smoke.sh [3],[7.2]. Each derives its dir from `stageMonorepo(t)`, sets `COMPOSE_ENV_FILES` via `cenvkit compose config`, and asserts the resolved value (WEB_PORT==18080, API_PORT==19090, STACK_TIER, IS_DEV, etc.). Guard all of these with `if !dockerAvailable() { t.Skip(...) }`.
  - **W3 value-level guard (docker-gated; the real W3 acceptance pin — finding [15] override).** Assert secrets-last AT THE VALUE LEVEL WITHIN THE CHAIN (cenvkit's actual responsibility — chain ordering — NOT "secret beats a Layer-2 service env_file"). In a staged fixture, set `API_TOKEN=base-val` in `.env` and `API_TOKEN=secret-real` in `.secrets.env` (both Layer-1; `.secrets.env` is last in the chain). Run `cenvkit compose config` and assert the rendered `API_TOKEN` value is `secret-real` (the later `.secrets.env` wins by `COMPOSE_ENV_FILES` last-wins). This is RED on any assembly that puts `.secrets.env` before `.env` within Layer-1. (NOTE per the §4c decision: cenvkit controls file ORDER only; a Layer-2 service `env_file:` reusing a chain var name wins per compose's last-wins — that is documented, not prevented, and is NOT what this guard asserts. The pure-Go file-ordering/dedup is covered by Task 4's `TestAssemble_OrderDedupSecretsLast`.)

- [ ] **Step 6: Confirm the exact assertion count is exactly 60 (S4 — recounted).** Count ported `smoke-monorepo` assertions; it MUST be **exactly 60**. Arithmetic (lead-verified on disk by running the suite): `test/smoke-monorepo.sh` self-reports `passed: 61` (`SMOKE_SKIP_DOCKER=1`), and the per-scenario PASS tally sums to exactly 61 — that is the verified baseline. The G1–G5 inversions for scenarios 9, 10, 22 are **polarity flips** (count-neutral — still 1, 2, 2 assertions); G3 drops **11.2** (untestable `COMPOSE_DEPTH=4` knob under the include-graph) → **−1**. So 61 − 1 = **60**. The count guard test itself must be RED against a pre-fix 61-asserting port. The C1 (Step 3b) and D1-runtime (Step 3c) guards use separate throwaway fixtures and are NOT counted in the 60. **Lead signs off on N=60 when approving the folded plan.** Update spec §13.1 / acceptance #1 accordingly (already folded to 60).

- [ ] **Step 7: Run the full acceptance suite both ways**

Run:
```bash
SMOKE_SKIP_DOCKER=1 go test ./test/ -v   # no-docker subset
go test ./test/ -v                        # full (requires docker)
go test ./...                             # everything green
```
Expected: green in both modes (docker subset skipped when unavailable).

- [ ] **Step 8: Commit**

```bash
git add test/cenvkit-acceptance_test.go test/seam_test.go
git commit -m "test(acceptance): port smoke suites to cenvkit (G1-G5 inversions encoded)"
```

---

## Task 8: Distribution — vendor shim, goreleaser, CI

**Owner:** go-engineer. **Depends on:** T6.

**Files:**
- Create: `cenvkit` (POSIX shim), `.goreleaser.yaml`, `.github/workflows/ci.yml`

- [ ] **Step 1: Vendored-mode POSIX shim `cenvkit`** (spec §6; `sh -n` clean; no bashisms)

```sh
#!/bin/sh
# Vendored-mode launcher: runs cenvkit from source via the Go toolchain.
# For lower latency, `go build -o .cenvkit.bin ./cmd/cenvkit` and run that
# (add .cenvkit.bin to .gitignore). No committed binaries, no network.
set -eu
dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec go run "$dir/cmd/cenvkit" "$@"
```
Verify: `sh -n cenvkit`.

> **Binary-name collision warning.** A bare `go build ./cmd/cenvkit` emits a binary literally named `cenvkit` in the repo root — colliding with this TRACKED POSIX shim (also `cenvkit`, `git add cenvkit` in Step 5). The documented fast path MUST therefore always use `go build -o .cenvkit.bin ./cmd/cenvkit` (namespaced output), and the `.gitignore` step below must NOT blanket-ignore a bare `cenvkit` (that would untrack the committed shim). Re-run `git status` after a fast-path build to confirm no binary is staged.

- [ ] **Step 1b: Add build artifacts to `.gitignore`** (routed to **architect/lead** — `.gitignore` is repo-config per the module-boundary table; go-engineer only with lead approval). Append:

```gitignore
# cenvkit local build artifacts — never commit a built binary (spec: no committed binaries)
.cenvkit.bin
/dist/            # goreleaser output
```
Do NOT add a bare `cenvkit` entry (see the collision warning above).

- [ ] **Step 2: `.goreleaser.yaml`** — per-OS binaries for `cmd/cenvkit` on GH release, with `-ldflags "-X main.version={{.Version}}"`. Validate with `goreleaser check` (and `goreleaser release --snapshot --clean` dry-run if installed).

- [ ] **Step 2b: Commit a minimal `.golangci.yml`** (no config exists in-repo; without one `golangci-lint run` is non-reproducible and the CI hedge means lint may silently never run — finding [18]). Enable at minimum the linters the spec DoD implies (spec lines ~263/336, "`go vet` + `gofmt` + `golangci-lint` clean"):

```yaml
linters:
  enable:
    - govet
    - errcheck
    - staticcheck
    - gofmt
    - ineffassign
    - unused
```

- [ ] **Step 3: `.github/workflows/ci.yml`** — matrix `go test ./...`, `go vet ./...`, `gofmt -l .` (fail if non-empty), the compose-go seam check from Task 3 Step 8 (paste the **corrected** `go list` form — NO `-deps`), `golangci-lint run` (mandatory, failing gate — `.golangci.yml` from Step 2b makes it deterministic; NOT "if configured"), and `goreleaser check`. Add a docker-enabled job running the full acceptance suite; a no-docker job running `SMOKE_SKIP_DOCKER=1`.

- [ ] **Step 4: Verify install paths**

```bash
go run ./cmd/cenvkit version        # local
sh ./cenvkit version                # vendored shim
```
Expected: both print the version.

- [ ] **Step 5: Commit**

```bash
git add cenvkit .goreleaser.yaml .github/workflows/ci.yml .golangci.yml .gitignore
git commit -m "build: vendored shim, goreleaser, CI (test/vet/fmt/seam/acceptance)"
```
> `.gitignore` is repo-config (lead-owned); group it into the lead's commit per the module boundaries.

---

## Task 9: Docs + flip default + deprecate sh kit

**Owner:** architect (docs are the lead's zone) + go-engineer (any code-adjacent README). **Depends on:** T7.

- [ ] **Step 1:** Update `README.md` + `docs/` so `cenvkit` is the documented default; add an install section (both modes, with the `go build -o .cenvkit.bin` fast-path note for vendored mode — audit W4; the default `go build` output name collides with the tracked `cenvkit` shim, so always namespace it). Document `COMPOSE_DEPTH` as accepted-but-ignored and the over-discovery removal (the migration win, spec §8). State the D1 behavior (lenient assembly, upstream runtime) and the `env_file:`-path resolution model (Layer-1-only refs, §4a). **Document two deliberate v1 migration deltas:**
  - **`env-debug --value`/`--trace` are RAW last-wins literals (Option B, thin).** Unlike the sh kit (which shell-sourced the chain and expanded `${VAR:-default}`/`${VAR:+x}`), v1 returns the literal stored value with NO default/cross-ref expansion. For fully-resolved values use `cenvkit env-debug --effective` (= `docker compose config`). (spec §5; Task 6A.)
  - **Do NOT reuse secret variable names in service `env_files`.** cenvkit controls file ORDER only; `docker compose` owns last-wins precedence over `COMPOSE_ENV_FILES`. "Secrets last" is a within-Layer-1 guarantee; a Layer-2 service `env_file:` that reuses a secret var name will win (Layer-2 is emitted after Layer-1) — documented, not prevented (spec §4c).
- [ ] **Step 2:** Mark the sh kit (`bin/docker`, `lib/`, `mk/`, `scripts/`) deprecated-but-retained-one-release in `docs/` + `CHANGELOG`. Do NOT delete it in v1 (it remains the parity reference).
- [ ] **Step 3:** Update `CHANGELOG`. Update the spec status line to "implemented (v1)".
- [ ] **Step 4: Commit** (lead).

```bash
git add README.md docs/ CHANGELOG.md
git commit -m "docs: cenvkit is the default; deprecate sh kit (retained one release)"
```

---

## Self-review (run by the author against the spec)

**Spec coverage:**
- §2 language/engine/pin → T1, T3 (v2.11.0 pinned + seam). ✓
- §4 core algorithm → T2 (Layer-1) + T3 (Layer-2) + T4 (merge). ✓
- §4a resolution model (C1) → T3 (paths interpolate from `in.Env`=Layer-1 only); C1 acceptance is **done in T7 Step 3b** (a path referencing a Layer-2-only var does not silently resolve; separate throwaway fixture, not counted in the 60). ✓
- §4b D1 (lever + both-halves acceptance) → T3 Step 2 (lenient+filter) + **T7 Step 3c** (runtime-fatal half, docker-gated). ✓
- §4c precedence/dedup (W3) → T4 covers file ordering/dedup; the W3 value-level (within-chain) guard is in **T7 Step 5** (docker-gated). ✓
- §5 CLI surface (compose/env-files/env-debug/validate/init/version; COMPOSE_DEPTH ignored) → T6; `version`+`--project-dir` are test-pinned in **T1 Step 3b** (`main_test.go`). ✓
- §6 distribution → T8 (incl. `.gitignore` build-artifacts step, `.golangci.yml`). ✓
- §9 errors policy (S2: skip missing chain files silently; fatal on malformed chain / compose-go load failure) → chain skips missing (T2); engine wraps load failure as fatal (T3). ✓
- §9 W1 sanitization RED → T2 Step 1. §9 W5 no-clobber RED → T5 Step 1. ✓
- §10 upstream-fidelity / pin → T3 Step 1 + T8 CI; compose-go-bump COMPOSE_FILE-seam re-confirm note added to spec §10. ✓
- §13 acceptance (**60** after S4 recount, G1–G5, W1/W3/W5/C1/D1 guards) → T7. ✓

**Gaps found & fixed inline:**
1. **C1 acceptance assertion** — **done in T7 Step 3b** (executable checkbox; a tiny throwaway fixture asserting an `env_file:` path referencing a Layer-2-only var does not silently resolve, pinning the §4a single-pass contract; RED against a two-pass impl). ✓
2. **S3 `validate` semantics** — resolved: default validates the currently-resolved env; `--all` does dev+prod, **re-resolving the Layer-1 chain per env** via `assemble(cmd, "COMPOSE_ENV="+env)` (T6 `newValidateCmd`; finding [3]/[9]). ✓
3. **Error policy S2** — chain malformed vs missing: `chain.Resolve` returns an error on an unreadable `.docker-env-chain` (fatal) but skips missing referenced files (parity). Engine load failure is fatal. Stated in T2/T3. ✓
4. **D1 runtime-fatal half** — **done in T7 Step 3c** (docker-gated; missing required env_file errors at the real `docker compose` run). ✓
5. **`.golangci.yml`** — added (T8 Step 2b); CI `golangci-lint run` is now mandatory (no "if configured" hedge), satisfying the spec DoD (spec ~263/336). ✓

**Placeholder scan:** no `TBD`/`handle edge cases`/"similar to Task N" — code shown in full per step. The COMPOSE_FILE-overlay handling (T3 Step 9) is now the mandatory primary path via the shared `resolveComposeFiles` resolver (no decision-rule branch); the only remaining empirical step is reconciling the exact monorepo fixture basenames in T3 Step 7, an explicit verification step.

**Type consistency:** `chain.Result{Files,Vars,ComposeEnv,Host}`, `engine.Input{ProjectDir,ConfigFiles,Env,Profiles}`, `engine.Result{EnvFiles,Project}`, `engine.ProjectView{WorkingDir,Services}`, `engine.HasComposeFile`/`engine.HasComposeFileEnv`/`resolveComposeFiles`, `envfiles.Assemble(layer1,layer2)`/`envfiles.Join`, `debug.Value/Trace/PrintChain/PrintFiles/Diff`, `bootstrap.Init(dir)` — names are identical across every task that references them. ✓

**Graph-label ↔ heading identity:** every Task-dependency-graph node label matches its `## Task N:` heading text — T4 = `internal/envfiles` (merge), T6 = `internal/debug + cmd/cenvkit wiring`. ✓ (Numeric `blockedBy` edges were already correct.)
