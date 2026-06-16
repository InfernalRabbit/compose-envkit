# cenvkit v2 — Rich Provenance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `cenvkit env-debug` "real": for any variable show which file set the winning value, what it overrode, and where its `${VAR}` took effect in the compose model; for any service show its effective environment with the source of each value — all in-process (no docker daemon), human output + `--json`.

**Architecture:** A pure-Go `internal/provenance` package owns the public data model + rendering. `internal/engine` (the only compose-go importer) gains `Provenance(ctx, ProvInput) (provenance.Report, error)` that does all compose-go work — a single non-interpolated raw load + dict walk + per-leaf `template.Substitute` for interpolation effects (B-lite), a resolved-model load for per-service env (C), and `dotenv` parsing for chain attribution (A). `cmd/cenvkit` rewires `env-debug` to it; the v1 raw `internal/debug` modes are superseded.

**Tech Stack:** Go 1.26 · `compose-go/v2 v2.11.0` (`cli`, `loader`, `dotenv`, `template`) · cobra · table-driven tests + golden JSON. Mechanism verified in `.claude/artifacts/compose-go-provenance-probe.md`.

---

## Source documents (read before executing)

- **Spec (authoritative):** `docs/superpowers/specs/2026-06-16-cenvkit-rich-provenance-design.md`
- **Probe (verified compose-go mechanism + exact API):** `.claude/artifacts/compose-go-provenance-probe.md`
- **v1 design (context, the engine seam, D1 lever):** `docs/superpowers/specs/2026-06-15-cenvkit-go-rewrite-design.md`
- **v1 engine impl (you are extending it):** `internal/engine/engine.go`, `internal/engine/discover.go`

## Conventions

- **Engine seam:** compose-go (incl. `loader`/`dotenv`/`template`) may be imported ONLY by `internal/engine` — CI seam check enforces it. `internal/provenance` is pure Go (imports neither compose-go nor `engine`); **`engine` imports `provenance`** for the shared data types (so `provenance` stays a fast, dependency-light leaf).
- **D1 lever is load-bearing for C:** the provenance resolved-load MUST keep `cli.WithoutEnvironmentResolution`, or `ServiceConfig.Environment` becomes env_file-merged and C is unattributable (probe §6).
- **Determinism:** all map iteration is sorted before emit (service names, var names, env keys, effects) so human + `--json` output is stable (a contract acceptance pins).
- TDD; commit after each green task; `gofmt`/`go vet` clean; no committed binaries.

## Execution & ownership (compose-envkit team)

- `*_test.go` → **qa-engineer**; production `.go` → **go-engineer**; `git` + docs + integration → **architect (lead)**.
- **Task 2 (engine) is a sensitive seam-contract change → run it through a PLAN-MODE teammate** (fresh agent, `mode:"plan"`, read-only plan → lead approves → implement), per the team risk-gate.
- Teammates do NOT commit; they report files + verification output, lead verifies on disk and commits.

### Dependency graph (for `TaskCreate` blockedBy)

```
T1 provenance (types+render) ──┬─> T2 engine.Provenance ──> T3 cmd wiring ──> T4 acceptance ──> T5 docs
                               └─────────────────────────────^ (T3 blockedBy T1,T2)
```
- **T1** provenance — no deps. **T2** engine — blockedBy T1. **T3** cmd — blockedBy T1,T2. **T4** acceptance — blockedBy T3. **T5** docs — blockedBy T4.

---

## File structure

| Path | Responsibility | compose-go? |
|---|---|---|
| `internal/provenance/model.go` | public types: `Source`, `Effect`, `VarTrace`, `EnvEntry`, `ServiceEnv`, `Report` + `ProvInput` re-exported? no — `ProvInput` lives in engine | no |
| `internal/provenance/render.go` | `RenderHuman(w, r, opts)` + `RenderJSON(w, r)` | no |
| `internal/provenance/*_test.go` | render + model tests (hand-built `Report` fixtures, golden JSON) | no |
| `internal/engine/provenance.go` | `ProvInput`, `Provenance(...)`, dict walk, A/B-lite/C, `parseDotEnv` | **yes** |
| `internal/engine/provenance_test.go` | `Provenance` over fixtures | no (drives engine) |
| `cmd/cenvkit/main.go` (modify) | `env-debug` rewired to provenance; `--json`/`--service` flags | no |
| `internal/debug/debug.go` (modify/shrink) | v1 raw `Value/Trace/Diff` removed (superseded); keep nothing engine needs | no |
| `test/cenvkit-acceptance_test.go` (modify) | new provenance assertions | no |

---

## Task 1: `internal/provenance` — model + rendering (pure Go)

**Owner:** go-engineer (impl) + qa-engineer (tests). **Depends on:** none.

**Contract (types — used verbatim by engine + cmd):**
```go
package provenance

type Source struct {
	File  string `json:"file"`
	Layer string `json:"layer"` // layer1 | layer2 | env_file | environment
}
type Effect struct {
	Service  string `json:"service"`
	Field    string `json:"field"`
	Resolved string `json:"resolved"`
}
type VarTrace struct {
	Name       string   `json:"name"`
	Value      string   `json:"value"`
	Winner     Source   `json:"winner"`
	Overridden []Source `json:"overridden,omitempty"`
	Effects    []Effect `json:"effects,omitempty"`
}
type EnvEntry struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source Source `json:"source"`
}
type ServiceEnv struct {
	Service string     `json:"service"`
	Entries []EnvEntry `json:"entries"`
}
type Report struct {
	Files      []string            `json:"files"`              // full merged COMPOSE_ENV_FILES (Layer-1 + Layer-2)
	ChainFiles []string            `json:"chain_files"`        // Layer-1-only subset, chain order (engine fills from in.EnvFiles where Layer=="layer1"); --chain renders this
	Vars       map[string]VarTrace `json:"vars"`               // A + B-lite
	Services   []ServiceEnv        `json:"services,omitempty"` // C (empty in chain-only mode)
}
```

- [ ] **Step 1: Write the failing JSON render test (golden, stable schema)**

`internal/provenance/render_test.go`:
```go
package provenance

import (
	"bytes"
	"strings"
	"testing"
)

func sampleReport() Report {
	return Report{
		Files: []string{"/p/.env", "/p/.secrets.env", "/p/web/.web.env"},
		Vars: map[string]VarTrace{
			"APP_PORT": {
				Name: "APP_PORT", Value: "8080",
				Winner:     Source{File: "/p/web/.web.env", Layer: "layer2"},
				Overridden: []Source{{File: "/p/.env", Layer: "layer1"}},
				Effects:    []Effect{{Service: "web", Field: "ports[0]", Resolved: "8080:80"}},
			},
		},
		Services: []ServiceEnv{{
			Service: "web",
			Entries: []EnvEntry{{Key: "APP_PORT", Value: "8080", Source: Source{File: "/p/web/.web.env", Layer: "env_file"}}},
		}},
	}
}

func TestRenderJSON_Stable(t *testing.T) {
	var b bytes.Buffer
	if err := RenderJSON(&b, sampleReport()); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{`"name": "APP_PORT"`, `"winner"`, `"field": "ports[0]"`, `"resolved": "8080:80"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("JSON missing %q:\n%s", want, got)
		}
	}
}

func TestRenderHuman_TraceShowsWinnerAndEffects(t *testing.T) {
	var b bytes.Buffer
	RenderHuman(&b, sampleReport(), HumanOpts{Trace: "APP_PORT"})
	got := b.String()
	for _, want := range []string{"APP_PORT=8080", "web/.web.env", "ports[0]", "8080:80"} {
		if !strings.Contains(got, want) {
			t.Fatalf("human --trace missing %q:\n%s", want, got)
		}
	}
}
```

- [ ] **Step 2: Run — verify FAIL** (`go test ./internal/provenance/` → undefined: RenderJSON/RenderHuman/HumanOpts)

- [ ] **Step 3: Implement `internal/provenance/model.go`** (the types block above) and **`internal/provenance/render.go`**:
```go
package provenance

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// HumanOpts selects which view RenderHuman emits.
type HumanOpts struct {
	Trace     string // non-empty: single-var trace (A + B-lite)
	Effective bool   // per-service env (C)
	Service   string // filter Effective to one service
	Chain     bool   // Files as Layer-1 list
	Files     bool   // full Files list
	Value     string // non-empty: print the winning value only
}

// RenderJSON writes the whole Report as indented JSON.
func RenderJSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// RenderHuman writes the selected view as plain text.
func RenderHuman(w io.Writer, r Report, o HumanOpts) {
	switch {
	case o.Value != "":
		fmt.Fprintln(w, r.Vars[o.Value].Value)
	case o.Trace != "":
		renderTrace(w, r, o.Trace)
	case o.Effective:
		renderEffective(w, r, o.Service)
	case o.Files:
		for _, f := range r.Files {
			fmt.Fprintln(w, f)
		}
	default: // Chain == default view: Layer-1 only (v1 semantics; secrets last WITHIN Layer-1)
		for _, f := range r.ChainFiles {
			fmt.Fprintln(w, f)
		}
	}
}

func renderTrace(w io.Writer, r Report, name string) {
	vt, ok := r.Vars[name]
	if !ok {
		fmt.Fprintf(w, "%s: not set\n", name)
		return
	}
	fmt.Fprintf(w, "%s=%s\n", name, vt.Value)
	fmt.Fprintf(w, "  winner:     %s (%s)\n", vt.Winner.File, vt.Winner.Layer)
	for _, s := range vt.Overridden {
		fmt.Fprintf(w, "  overridden: %s (%s)\n", s.File, s.Layer)
	}
	for _, e := range vt.Effects {
		fmt.Fprintf(w, "  effect:     service %s field %s -> %s\n", e.Service, e.Field, e.Resolved)
	}
}

func renderEffective(w io.Writer, r Report, service string) {
	for _, se := range r.Services {
		if service != "" && se.Service != service {
			continue
		}
		fmt.Fprintf(w, "service %s:\n", se.Service)
		for _, e := range se.Entries {
			fmt.Fprintf(w, "  %s=%s\t<- %s (%s)\n", e.Key, e.Value, e.Source.File, e.Source.Layer)
		}
	}
	_ = sort.Strings // entries are pre-sorted by the engine; keep deterministic
}
```

- [ ] **Step 4: Run — verify PASS** (`go test ./internal/provenance/ -v`)

- [ ] **Step 5: Commit** — `git add internal/provenance/ && git commit -m "feat(provenance): report model + human/JSON rendering"`

---

## Task 2: `internal/engine` — `Provenance` (compose-go work) · PLAN-MODE

**Owner:** go-engineer (impl, via a PLAN-MODE teammate for the engine contract) + qa-engineer (tests). **Depends on:** T1.

**Files:** Create `internal/engine/provenance.go`, `internal/engine/provenance_test.go`.

**Contract:**
```go
// in internal/engine/provenance.go
type ProvFile struct{ Path, Layer string } // Layer: "layer1" | "layer2"
type ProvInput struct {
	ProjectDir  string
	ConfigFiles []string   // explicit -f; empty => resolveComposeFiles
	Env         []string   // chain Vars "K=V" (interpolation + mapping source)
	Profiles    []string
	EnvFiles    []ProvFile // ordered merged COMPOSE_ENV_FILES (for A attribution)
}

// added to the Engine interface:
//   Provenance(ctx context.Context, in ProvInput) (provenance.Report, error)
```

- [ ] **Step 1: Write the failing B-lite + A + C test (RED)**

`internal/engine/provenance_test.go`:
```go
package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/compose-envkit/compose-envkit/internal/engine"
)

func writeF(t *testing.T, dir, rel, body string) string {
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

func TestProvenance_BLite_And_C(t *testing.T) {
	dir := t.TempDir()
	writeF(t, dir, "compose.yaml", `
services:
  web:
    image: busybox
    ports: ["${WEB_PORT}:80"]
    environment:
      TIER: "${COMPOSE_ENV}"
    env_file: [./web.env]
`)
	// WEB_PORT lives ONLY in the Layer-2 env_file (NOT in the env slice), so the
	// effect assertion below is RED on the pre-fix Layer-1-only mapping (which
	// never saw WEB_PORT) and GREEN once the merged-env fix lands.
	web := writeF(t, dir, "web.env", "WEB_ONLY=yes\nTIER=fromfile\nWEB_PORT=8080\n")
	env := []string{"COMPOSE_ENV=staging"}

	rep, err := engine.New().Provenance(context.Background(), engine.ProvInput{
		ProjectDir: dir,
		Env:        env,
		EnvFiles:   []engine.ProvFile{{Path: web, Layer: "layer2"}},
	})
	if err != nil {
		t.Fatalf("Provenance: %v", err)
	}

	// B-lite (Layer-2-only var effect): WEB_PORT is set ONLY in web.env, yet its
	// ${WEB_PORT} effect on web.ports[0] must still resolve to 8080:80 — proving
	// the interpolation mapping reads the MERGED COMPOSE_ENV_FILES, not in.Env alone.
	wp := rep.Vars["WEB_PORT"]
	if len(wp.Effects) == 0 || wp.Effects[0].Service != "web" || wp.Effects[0].Resolved != "8080:80" {
		t.Fatalf("WEB_PORT effect wrong: %+v", wp.Effects)
	}
	// C: inline environment TIER overrides env_file TIER; WEB_ONLY from env_file
	var svc *struct{}
	_ = svc
	web_ := findService(rep, "web")
	if got := entry(web_, "TIER"); got.Value != "staging" || got.Source.Layer != "environment" {
		t.Fatalf("TIER should be inline 'staging', got %+v", got)
	}
	if got := entry(web_, "WEB_ONLY"); got.Value != "yes" || got.Source.Layer != "env_file" {
		t.Fatalf("WEB_ONLY should be env_file 'yes', got %+v", got)
	}
	// A: WEB_ONLY winner is web.env
	if rep.Vars["WEB_ONLY"].Winner.File != web {
		t.Fatalf("WEB_ONLY winner = %q, want %q", rep.Vars["WEB_ONLY"].Winner.File, web)
	}
}
```
> qa: add `findService(rep, name)` / `entry(se, key)` test helpers in the same file (return the `provenance.ServiceEnv` / `provenance.EnvEntry`; `t.Fatal` if absent). Also add a chain-only case: no compose file in `dir` → `Provenance` returns a `Report` with `Vars` (A) populated and empty `Services`/`Effects`, no error (gate via `HasComposeFileEnv`).
> qa (Layer-2-only var effect — the RED guard for findings [0]/[2]): the test above is deliberately authored so `WEB_PORT` is sourced ONLY from the Layer-2 `web.env` (env_file) and is ABSENT from the `env` slice. On pre-fix code (mapping built from `in.Env` alone) the `${WEB_PORT}` effect resolves wrong (no value → empty/`:80`), so `Effects[0].Resolved == "8080:80"` FAILS; once the merged-env fix lands it PASSES. This is the temp-revert RED guard that the gap can no longer hide behind a pre-injected env. Keep `WEB_PORT` out of `env` — do not "helpfully" re-add it.

- [ ] **Step 2: Run — verify FAIL** (`undefined: engine.ProvInput / Provenance`)

- [ ] **Step 3: Implement `internal/engine/provenance.go`** (verified mechanism — probe §2/§6):
```go
package engine

import (
	"context"
	"fmt"
	"sort"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/template"
	"github.com/compose-spec/compose-go/v2/types"

	"github.com/compose-envkit/compose-envkit/internal/provenance"
)

type ProvFile struct{ Path, Layer string }
type ProvInput struct {
	ProjectDir  string
	ConfigFiles []string
	Env         []string
	Profiles    []string
	EnvFiles    []ProvFile
}

// parseDotEnv uses compose-go's own dotenv parser (docker-compose parity).
// lookup lets in-file ${VAR} refs that the chain provides resolve quietly
// (mirrors the WithoutLogging discipline of the B-lite Substitute path). A
// genuinely-unset external ${VAR} inside an env_file STILL warns — that is
// correct docker-compose parity (last-wins COMPOSE_ENV_FILES); only
// chain-provided vars are silenced.
func parseDotEnv(path string, lookup dotenv.LookupFn) (map[string]string, error) {
	return dotenv.ReadFile(path, lookup)
}

func envSliceToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if i := indexByte(kv, '='); i > 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}
func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func (e *composeEngine) Provenance(ctx context.Context, in ProvInput) (provenance.Report, error) {
	configs := in.ConfigFiles
	if len(configs) == 0 {
		configs = resolveComposeFiles(in.ProjectDir, in.Env)
	}

	rep := provenance.Report{Vars: map[string]provenance.VarTrace{}}

	// --- merged interpolation env (mirror docker compose: read COMPOSE_ENV_FILES
	// before interpolation). Build chainEnv = union of every EnvFiles entry in
	// declaration order (later file wins, so Layer-2 over Layer-1), THEN overlay
	// in.Env (OS / Layer-1 chain seed) LAST so the OS/shell value wins — parity
	// with chain.go's OS-wins merge. We capture each file's parsed map once here
	// and REUSE it for the A-attribution loop below (no double-parse).
	chainEnv := map[string]string{}
	parsed := make([]map[string]string, len(in.EnvFiles)) // parsed[i] aligns with in.EnvFiles[i]
	lookup := dotenv.LookupFn(func(k string) (string, bool) { v, ok := chainEnv[k]; return v, ok })
	for i, f := range in.EnvFiles {
		// First pass without lookup (lookup is built incrementally); refs that
		// resolve later still win via the overlay. Practical chains rarely depend
		// on cross-file ${VAR} inside an env_file; the C/B-lite paths reparse with
		// the full lookup below.
		if m, err := parseDotEnv(f.Path, lookup); err == nil {
			parsed[i] = m
			for k, v := range m {
				chainEnv[k] = v // later file in declaration order wins
			}
		}
	}
	for k, v := range envSliceToMap(in.Env) {
		chainEnv[k] = v // OS / Layer-1 seed wins last
	}
	// mapping (B-lite) reads the merged effective env so "${WEB_PORT:-0}:80"
	// resolves to the real value (e.g. "18080:80"), not the :-0 default.
	mapping := template.Mapping(func(k string) (string, bool) { v, ok := chainEnv[k]; return v, ok })

	// --- A: chain attribution over the ordered merged COMPOSE_ENV_FILES ---
	for i, f := range in.EnvFiles {
		rep.Files = append(rep.Files, f.Path)
		m := parsed[i]
		if m == nil {
			continue // skip missing (parity)
		}
		src := provenance.Source{File: f.Path, Layer: f.Layer}
		for k, v := range m {
			vt := rep.Vars[k]
			vt.Name = k
			if vt.Winner.File != "" {
				vt.Overridden = append(vt.Overridden, vt.Winner)
			}
			vt.Winner, vt.Value = src, v
			rep.Vars[k] = vt
		}
	}

	// --- A overlay: shell environment is the FINAL last-wins source, so each
	// VarTrace's Winner/Value/Overridden stay mutually consistent with what
	// compose actually interpolates (chainEnv overlays files with OS env). When
	// the env overrides a file, the file becomes Overridden and the winner
	// becomes (environment); a shell-only var (no chain file set it) is attributed
	// to (environment) too. (chainEnv mixes genuine OS exports with file vars
	// promoted into rep.Vars; if cmd later wants to distinguish true-shell-only
	// keys it can pass the raw os.Environ() set in ProvInput — but at minimum the
	// reported Value must reflect the merged effective env, not the raw file literal.)
	envSrc := provenance.Source{File: "(environment)", Layer: "environment"}
	for k, v := range chainEnv {
		vt := rep.Vars[k]
		vt.Name = k
		if vt.Winner.File != "" && vt.Value != v {
			// a file set it but the shell overrides → shadow the file winner
			vt.Overridden = append(vt.Overridden, vt.Winner)
			vt.Winner, vt.Value = envSrc, v
		} else if vt.Winner.File == "" {
			// shell-only var (no chain file set it)
			vt.Winner, vt.Value = envSrc, v
		}
		rep.Vars[k] = vt
	}

	// chain-only mode: no compose file => return A only.
	if len(configs) == 0 {
		return rep, nil
	}

	// --- C: per-service env with sources (D1 lever keeps environment inline-only) ---
	// Feed the SAME merged env (sorted "K=V") to cli.WithEnv so inline
	// environment values like "${WEB_PORT:-0}" resolve to the real chain value,
	// consistent with the B-lite mapping above (was Layer-1-only via in.Env).
	mergedEnv := make([]string, 0, len(chainEnv))
	for _, k := range sortedKeys(chainEnv) {
		mergedEnv = append(mergedEnv, k+"="+chainEnv[k])
	}
	opts, err := cli.NewProjectOptions(configs,
		cli.WithWorkingDirectory(in.ProjectDir),
		cli.WithEnv(mergedEnv),
		cli.WithProfiles(in.Profiles),
		cli.WithResolvedPaths(true),
		cli.WithInterpolation(true),
		cli.WithoutEnvironmentResolution, // REQUIRED for C separability (probe §6)
	)
	if err != nil {
		return provenance.Report{}, fmt.Errorf("provenance options: %w", err)
	}
	proj, err := opts.LoadProject(ctx)
	if err != nil {
		return provenance.Report{}, fmt.Errorf("provenance load: %w", err)
	}
	svcNames := make([]string, 0, len(proj.Services))
	for n := range proj.Services {
		svcNames = append(svcNames, n)
	}
	sort.Strings(svcNames)
	for _, name := range svcNames {
		svc := proj.Services[name]
		final := map[string]string{}
		source := map[string]provenance.Source{}
		for _, ef := range svc.EnvFiles {
			m, perr := parseDotEnv(ef.Path, lookup)
			if perr != nil {
				continue
			}
			for k, v := range m {
				final[k] = v
				source[k] = provenance.Source{File: ef.Path, Layer: "env_file"}
			}
		}
		for k, vp := range svc.Environment { // map[string]*string; inline overrides
			val := ""
			if vp != nil {
				val = *vp
			}
			final[k] = val
			source[k] = provenance.Source{File: "(inline environment:)", Layer: "environment"}
		}
		se := provenance.ServiceEnv{Service: name}
		for _, k := range sortedKeys(final) {
			se.Entries = append(se.Entries, provenance.EnvEntry{Key: k, Value: final[k], Source: source[k]})
		}
		rep.Services = append(rep.Services, se)
	}

	// --- B-lite: single RAW load + dict walk + per-leaf Substitute ---
	details, err := loader.LoadConfigFiles(ctx, configs, in.ProjectDir)
	if err != nil {
		return provenance.Report{}, fmt.Errorf("provenance raw config: %w", err)
	}
	details.Environment = types.Mapping(chainEnv)
	rawDict, err := loader.LoadModelWithContext(ctx, *details, func(o *loader.Options) {
		o.SkipInterpolation = true
		o.SkipValidation = true
		o.SkipConsistencyCheck = true
		o.SkipResolveEnvironment = true
	})
	if err != nil {
		return provenance.Report{}, fmt.Errorf("provenance raw load: %w", err)
	}
	walkDict(rawDict, "", func(path, leaf string) {
		vars := template.ExtractVariables(map[string]any{"x": leaf}, template.DefaultPattern)
		if len(vars) == 0 {
			return
		}
		svc, field, ok := splitServiceField(path)
		if !ok {
			return
		}
		resolved, _ := template.SubstituteWithOptions(leaf, mapping, template.WithoutLogging)
		for vn := range vars {
			vt := rep.Vars[vn]
			vt.Name = vn
			vt.Effects = append(vt.Effects, provenance.Effect{Service: svc, Field: field, Resolved: resolved})
			rep.Vars[vn] = vt
		}
	})
	for k, vt := range rep.Vars { // deterministic effects order
		sort.Slice(vt.Effects, func(i, j int) bool {
			if vt.Effects[i].Service != vt.Effects[j].Service {
				return vt.Effects[i].Service < vt.Effects[j].Service
			}
			return vt.Effects[i].Field < vt.Effects[j].Field
		})
		rep.Vars[k] = vt
	}
	return rep, nil
}

func sortedKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
```

- [ ] **Step 4: Implement the dict walk + path split (`internal/engine/provenance.go`, same file)**
```go
import "fmt" // already imported above

// walkDict recurses map[string]any / []any and calls fn on every string leaf
// with a dotted+[i] path (e.g. "services.web.ports[0]"). Map keys are sorted for
// deterministic traversal.
func walkDict(node any, path string, fn func(path, leaf string)) {
	switch v := node.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p := k
			if path != "" {
				p = path + "." + k
			}
			walkDict(v[k], p, fn)
		}
	case []any:
		for i, child := range v {
			walkDict(child, fmt.Sprintf("%s[%d]", path, i), fn)
		}
	case string:
		fn(path, v)
	}
}

// splitServiceField turns "services.web.ports[0]" into ("web", "ports[0]", true).
// It INTENTIONALLY returns ok=false for non-service paths (top-level
// networks/volumes/configs/secrets/x-*); those ${VAR} effects are out of scope
// for B-lite (see spec §2/§8) and are deliberately dropped, not a bug. Such a
// var still appears in A (chain attribution) if it is a COMPOSE_ENV_FILES key;
// only its Effects entry is omitted.
func splitServiceField(path string) (service, field string, ok bool) {
	const p = "services."
	if len(path) <= len(p) || path[:len(p)] != p {
		return "", "", false
	}
	rest := path[len(p):]
	i := indexByte(rest, '.')
	if i < 0 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}
```

- [ ] **Step 5: Add `Provenance` to the `Engine` interface** in `internal/engine/engine.go`:
```go
type Engine interface {
	Resolve(ctx context.Context, in Input) (Result, error)
	Provenance(ctx context.Context, in ProvInput) (provenance.Report, error)
}
```

- [ ] **Step 6: Run tests + seam check + vet/fmt**

Run:
```bash
go test ./internal/engine/ -run Provenance -v
# seam: provenance must NOT import compose-go; engine still the only importer
MOD=$(go list -m); go list -f '{{.ImportPath}} {{join .Imports " "}}' ./... | grep -v "^$MOD/internal/engine " | grep 'compose-spec/compose-go' && { echo LEAK; exit 1; } || echo "seam OK"
go vet ./internal/engine/ ./internal/provenance/ && gofmt -l internal/engine internal/provenance
```
Expected: PASS · `seam OK` · clean. (If `internal/provenance` shows a compose-go import, a type leaked — move it.)

- [ ] **Step 7: Commit** — `git add internal/engine/provenance.go internal/engine/provenance_test.go internal/engine/engine.go && git commit -m "feat(engine): Provenance (A chain attribution + B-lite effects + C per-service)"`

---

## Task 3: `cmd/cenvkit` — rewire `env-debug` to provenance

**Owner:** go-engineer (impl) + qa-engineer (tests). **Depends on:** T1, T2.

**Files:** Modify `cmd/cenvkit/main.go`; shrink `internal/debug/debug.go` (remove the v1 raw `Value`/`Trace`/`Diff` — superseded; the package may be deleted if nothing else uses it — check `grep -rn internal/debug cmd/`).

- [ ] **Step 1: Replace `newEnvDebugCmd`** so it builds the provenance report and renders it. Assemble the ordered `EnvFiles` (Layer-1 from chain, Layer-2 from `engine.Resolve`) and pass to `engine.Provenance`:
```go
func newEnvDebugCmd() *cobra.Command {
	var (
		mChain, mEffective, mFiles, mTrace, mValue, jsonOut bool
		varName, service                                    string
	)
	c := &cobra.Command{
		Use:   "env-debug",
		Short: "Inspect the resolved env chain with provenance",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := resolveProjectDir(cmd)
			if err != nil {
				return err
			}
			cr, err := chain.Resolve(chain.Input{ProjectDir: dir, OSEnv: os.Environ(), Hostname: os.Hostname})
			if err != nil {
				return err
			}
			// ordered merged COMPOSE_ENV_FILES with layer labels
			pf := make([]engine.ProvFile, 0, len(cr.Files))
			for _, f := range cr.Files {
				pf = append(pf, engine.ProvFile{Path: f, Layer: "layer1"})
			}
			var er engine.Result
			if engine.HasComposeFileEnv(dir, cr.Vars) {
				er, err = engine.New().Resolve(cmd.Context(), engine.Input{
					ProjectDir: dir, Env: cr.Vars, Profiles: splitProfiles(envValue(cr.Vars, "COMPOSE_PROFILES")),
				})
				if err != nil {
					return err
				}
				for _, f := range er.EnvFiles {
					pf = append(pf, engine.ProvFile{Path: f, Layer: "layer2"})
				}
			}
			rep, err := engine.New().Provenance(cmd.Context(), engine.ProvInput{
				ProjectDir: dir, Env: cr.Vars,
				Profiles: splitProfiles(envValue(cr.Vars, "COMPOSE_PROFILES")),
				EnvFiles: pf,
			})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if jsonOut {
				return provenance.RenderJSON(out, rep)
			}
			provenance.RenderHuman(out, rep, provenance.HumanOpts{
				Trace: pick(mTrace, varName), Value: pick(mValue, varName),
				Effective: mEffective, Service: service, Chain: mChain, Files: mFiles,
			})
			return nil
		},
	}
	c.Flags().BoolVar(&mChain, "chain", false, "Layer-1 chain files")
	c.Flags().BoolVar(&mEffective, "effective", false, "per-service env with sources")
	c.Flags().BoolVar(&mFiles, "files", false, "full COMPOSE_ENV_FILES list")
	c.Flags().BoolVar(&mTrace, "trace", false, "trace --var: winner, overridden, effects")
	c.Flags().BoolVar(&mValue, "value", false, "winning value of --var")
	c.Flags().StringVar(&varName, "var", "", "variable for --trace/--value")
	c.Flags().StringVar(&service, "service", "", "filter --effective to one service")
	c.Flags().BoolVar(&jsonOut, "json", false, "machine-readable JSON")
	return c
}

// pick returns name when enabled, else "".
func pick(enabled bool, name string) string {
	if enabled {
		return name
	}
	return ""
}
```
Add the `internal/provenance` import to `cmd/cenvkit/main.go`. Keep `--effective` docker-free (it no longer shells out to `docker compose config`; that path is gone — `--effective` is now provenance-backed).

- [ ] **Step 2: Remove superseded v1 raw debug** — delete `Value`/`Trace`/`Diff` from `internal/debug/debug.go` (and their tests, qa). If `internal/debug` is now unused by `cmd`, delete the package and its test file. Verify: `grep -rn "internal/debug" cmd/ internal/ | grep -v _test`.

- [ ] **Step 3: Build + fixture smoke + vet/fmt**

Run:
```bash
go build ./... && go vet ./... && gofmt -l .
( cd examples/monorepo && cp example.env .env && cp example.dev.env .dev.env && \
  go run ../../cmd/cenvkit env-debug --trace --var WEB_PORT && \
  go run ../../cmd/cenvkit env-debug --effective --service web --json ; \
  rm -f .env .dev.env )
```
Expected: clean build; `--trace --var WEB_PORT` shows winner + a `web` effect; `--effective --service web --json` emits valid JSON. (The `cp`/`rm` seeds Layer-1 in the fixture without dirtying tracked files — or use a temp copy.)

- [ ] **Step 4: Commit** — `git add cmd/cenvkit/main.go internal/debug/ && git commit -m "feat(cmd): env-debug backed by provenance (+ --json/--service); drop v1 raw debug"`

---

## Task 4: Acceptance — provenance assertions

**Owner:** qa-engineer. **Depends on:** T3.

- [ ] **Step 1: Add provenance acceptance tests** in `test/cenvkit-acceptance_test.go`, driving the built binary against a staged `examples/monorepo` (reuse `stageMonorepo(t)`):
  - `env-debug --trace --var WEB_PORT` → output contains the winning `.web.env` path AND a `service=web` effect with the resolved port. **[A + B-lite]** (2 assertions)
  - `env-debug --trace --var WEB_PORT --json` → parses; assert `Vars.WEB_PORT.winner.layer == "layer2"` and a non-empty `effects`. (Use `encoding/json` into a minimal struct.) (2 assertions)
  - `env-debug --effective --service web --json` → parses; assert an entry whose `source.layer` is `env_file` or `environment`. **[C]** (1 assertion)
  - `env-debug --value --var SMOKE_VAL` → winning value (**replaces** the existing v1 raw `--value` assertion — net 0 on count).
  - **W3 (provenance form):** a var set in both `.secrets.env` and an earlier Layer-1 file → `--trace --var ...` winner is `.secrets.env` (within-chain secrets-last). (1 assertion)
  - Chain-only: a dir with no compose file → `env-debug --trace --var X --json` succeeds, `services` empty. (2 assertions)
- [ ] **Step 2: Recount (concrete target = 68)** — these provenance assertions grow the smoke-monorepo total **from 60 to 68**. Derivation: baseline **60** + `--trace --var WEB_PORT` human (2) + `--trace --var WEB_PORT --json` (2) + `--effective --service web --json` (1) + W3-winner (1) + chain-only (2) + `--value --var SMOKE_VAL` (REPLACES the existing v1 raw `--value` assertion → net **0**) = **68**. This is an add-AND-replace recount, NOT pure addition — the `--value --var SMOKE_VAL` assertion supersedes the existing v1 raw assertion at `test/cenvkit-acceptance_test.go` (TestEnvDebug_Value), so it does not count as net-new. Audit the exact per-scenario total to confirm 68, then in the **Step 3 commit** update every hardcoded count site in `test/cenvkit-acceptance_test.go` to 68:
  - lines 1–2 header ("60 assertions after S4 recount" → "68 …"),
  - line 17 ("exactly 60 smoke-monorepo assertions (61 baseline − 1 for dropped 11.2)" → "68 …" with the updated derivation note: 60 baseline + 8 provenance net),
  - line 18 ("NOT counted in 60" → "NOT counted in 68"),
  - any per-scenario `(N assertions)` annotation whose set changed.
  These header files MUST be added to the Step 3 `git add`. **Lead signs off on the final integer (68).** Keep this as a concrete target, not an open TODO. Run both modes:
```bash
SMOKE_SKIP_DOCKER=1 go test ./... && go test ./...
```
Expected: green both modes (provenance tests are daemon-free; docker-gated v1 ones still gated).
- [ ] **Step 3: Commit** — lead: `git add test/cenvkit-acceptance_test.go && git commit -m "test(acceptance): env-debug provenance (trace/effective/json, W3 winner); count 60→68"` (the same file holds the new assertions AND the updated header count comments at lines 1–2/17/18 — stage it once).

---

## Task 5: Docs

**Owner:** architect (lead). **Depends on:** T4.

- [ ] **Step 1:** Update `docs/cenvkit.md` — rewrite the `env-debug` section: `--trace` (winner/overridden/effects), `--effective [--service]` (per-service env + sources), `--value`, `--json`; note it is **daemon-free** and supersedes v1's raw `--value/--trace` and the `docker compose config`-backed `--effective`. Add a short "Provenance model" subsection (the `Report` shape).
- [ ] **Step 2:** `CHANGELOG.md` — add under `[Unreleased]` a "rich provenance (v2)" entry. Note the **breaking CLI change: `--diff` removed** in v2 (superseded by `--trace --var` + `--effective`; spec §7 omits it) and the `--trace`/`--value` form now takes `--var VAR`. Flip the v2 spec status line to "implemented". Also reflect the `--diff` removal in `docs/cenvkit.md` (Step 1).
- [ ] **Step 3:** Commit (lead).

---

## Self-review (author, against the spec)

**Architecture reconciliation (Option A — spec updated to match this plan):** This plan's design is the chosen one: `engine.Provenance(ctx, ProvInput)` returns `provenance.Report` **directly**; chain attribution (A) is computed **inside the engine** over the cmd-supplied ordered `EnvFiles` (over the merged EFFECTIVE env = files + environment overlay, not files-only); `engine` imports `provenance` (one direction); `provenance` stays pure-Go. The intermediate `engine.ProvenanceFacts/ServiceEnvFacts/KVSource/EffectFact` types and the `internal/provenance.Build(chainAttr, facts)` step are **dropped** — they never exist in this plan. The spec (§4/§5/§6/§10) has been updated to this single-Report design; there is no exported `engine.ParseDotEnv` (engine uses an internal lowercase `parseDotEnv`).

**Spec coverage (against the UPDATED spec):** §2 A → T2 (chain attribution over the merged effective env) ✓ · §2 B-lite → T2 (raw load + walk + substitute over merged env) ✓ · §2 C → T2 (per-service, D1 lever, merged env to `cli.WithEnv`) ✓ · §3 use dotenv/template → T2 (`dotenv.ReadFile` with lookupFn, `template.ExtractVariables`/`SubstituteWithOptions`) ✓ · **§4 architecture — design is engine-owns-A / single `provenance.Report` (engine→provenance, provenance pure); this DIVERGED from the spec's old `ProvenanceFacts`/`Build`/chain-A sketch, which has now been deleted from the spec to match (Option A)** → file structure + T1/T2 ✓ · **§5 model → T1 only (`Report`; the engine-side `Facts` block is removed from the spec per Option A — no green check against the dropped contract)** ✓ · §6 flow → engine assembles A+B-lite+C directly into `Report` (no `Build` node); spec §6 step 3 updated to match ✓ · §7 CLI (`--trace --var`/`--effective`/`--value --var`/`--chain`/`--files`/`--service`/`--json`; **`--diff` removed**) → T3 ✓ · §9 errors (chain-only no-compose; load failure fatal; unset var "not set") → T2 (chain-only return; wrapped errors) + T1 (`renderTrace` "not set") ✓ · §10 testing (provenance render unit on hand-built `Report` fixtures, engine unit, golden JSON, acceptance, daemon-free) → T1/T2/T4 ✓ · §11 probe done → reflected in T2 code ✓.

**Gaps fixed inline:** (1) `--diff` (the v1 Layer-2-over-Layer-1 var diff) is **removed in v2** — spec §7 omits it; it is superseded by `--trace --var` (per-var winner/overridden/effects) and `--effective` (per-service env). Noted as a breaking CLI change in T5 docs. (No "alias" relationship is created — that never existed in v1.) (2) `--effective` no longer shells out to docker — stated in T3 Step 1 + T5 docs. (3) The interpolation env is built from the MERGED `COMPOSE_ENV_FILES` (files last-wins, then OS overlay) for B-lite mapping, `details.Environment`, AND the C-load `cli.WithEnv`, so Layer-2-only vars like `${WEB_PORT}` resolve correctly (findings [0]/[2]); A's reported Value/Winner/Overridden are made consistent via the final `(environment)` overlay (finding [3]).

**Placeholder scan:** none — engine mechanism is the probe-verified code; tests are concrete; the only injected helpers (`findService`/`entry`) are flagged for qa with their contract.

**Type consistency:** `provenance.{Source,Effect,VarTrace,EnvEntry,ServiceEnv,Report,HumanOpts}` and `engine.{ProvFile,ProvInput,Provenance}` are used identically across T1→T2→T3. `engine` imports `provenance` (one direction); seam check (T2 Step 6) guards that `provenance` never imports compose-go.
