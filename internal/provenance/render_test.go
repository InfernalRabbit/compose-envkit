package provenance

import (
	"bytes"
	"strings"
	"testing"
)

// sampleReport returns a v3 Report fixture.
//
// APP_PORT is a Layer-1 chain var (InChain=true): it resolves normally and its
// effect shows the real value.  GAP_VAR is a gap var (InChain=false): it is
// defined only in a service env_file: (RuntimeDefs), so its effect falls back
// and Gap=true.  Both paths are exercised by the render tests below.
func sampleReport() Report {
	return Report{
		Files:      []string{"/p/.env", "/p/.secrets.env"}, // Layer-1 only (v3)
		ChainFiles: []string{"/p/.env", "/p/.secrets.env"},
		Vars: map[string]VarTrace{
			"APP_PORT": {
				Name: "APP_PORT", Value: "8080",
				Winner:     Source{File: "/p/.env", Layer: "layer1"},
				Overridden: []Source{{File: "/p/.secrets.env", Layer: "layer1"}},
				Effects:    []Effect{{Service: "web", Field: "ports[0]", Resolved: "8080:80"}},
				InChain:    true,
			},
			"GAP_VAR": {
				Name:        "GAP_VAR",
				InChain:     false,
				RuntimeDefs: []ServiceVal{{Service: "web", File: "/p/web/.web.env", Value: "gapval"}},
				Effects:     []Effect{{Service: "web", Field: "environment[0]", Resolved: ":80", Gap: true}},
				Gap:         true,
			},
		},
		Services: []ServiceEnv{{
			Service: "web",
			Entries: []EnvEntry{{Key: "APP_PORT", Value: "8080", Source: Source{File: "/p/.env", Layer: "layer1"}}},
		}},
	}
}

func TestRenderJSON_Stable(t *testing.T) {
	var b bytes.Buffer
	if err := RenderJSON(&b, sampleReport()); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{`"name": "APP_PORT"`, `"winner"`, `"field": "ports[0]"`, `"resolved": "8080:80"`, `"in_chain": true`} {
		if !strings.Contains(got, want) {
			t.Fatalf("JSON missing %q:\n%s", want, got)
		}
	}
}

// TestRenderHuman_TraceInChain: in-chain var → normal winner/overridden/effects path.
func TestRenderHuman_TraceShowsWinnerAndEffects(t *testing.T) {
	var b bytes.Buffer
	RenderHuman(&b, sampleReport(), HumanOpts{Trace: "APP_PORT"})
	got := b.String()
	for _, want := range []string{"APP_PORT=8080", ".env", "ports[0]", "8080:80"} {
		if !strings.Contains(got, want) {
			t.Fatalf("human --trace (in-chain) missing %q:\n%s", want, got)
		}
	}
	// must NOT show the gap path
	if strings.Contains(got, "NOT in the Layer-1 chain") {
		t.Fatalf("human --trace (in-chain) must not show gap path:\n%s", got)
	}
}

// TestRenderHuman_TraceGapVar: gap var (InChain=false) → interpolation/runtime/gap path.
func TestRenderHuman_TraceGapVar(t *testing.T) {
	var b bytes.Buffer
	RenderHuman(&b, sampleReport(), HumanOpts{Trace: "GAP_VAR"})
	got := b.String()
	for _, want := range []string{"NOT in the Layer-1 chain", "web/.web.env", "gap"} {
		if !strings.Contains(got, want) {
			t.Fatalf("human --trace (gap) missing %q:\n%s", want, got)
		}
	}
	// must NOT show the in-chain winner first line ("GAP_VAR=<val>" as the opening line)
	if strings.HasPrefix(got, "GAP_VAR=") {
		t.Fatalf("human --trace (gap) must not open with in-chain VAR=val format:\n%s", got)
	}
}

// ─── --overview render tests ─────────────────────────────────────────────────

// overviewReport builds a Report.Layers fixture that exercises all three
// marker paths (+/~/·) across a chain section and a runtime service section,
// with a gap var, an inline environment: layer, and a header-ready setup.
func overviewReport() Report {
	return Report{
		Files:      []string{"/p/.env", "/p/.dev.env"},
		ChainFiles: []string{"/p/.env", "/p/.dev.env"},
		Vars: map[string]VarTrace{
			// SITE_URL is defined in .env (+) then overridden in .dev.env (~).
			"SITE_URL": {
				Name: "SITE_URL", Value: "dev.example.com",
				Winner:     Source{File: "/p/.dev.env", Layer: "layer1"},
				Overridden: []Source{{File: "/p/.env", Layer: "layer1"}},
				InChain:    true,
			},
			// WEB_PORT is gap (env_file-only) with a service effect.
			"WEB_PORT": {
				Name:        "WEB_PORT",
				InChain:     false,
				RuntimeDefs: []ServiceVal{{Service: "web", File: "/p/web/.web.env", Value: "18080"}},
				Effects:     []Effect{{Service: "web", Field: "ports[0]", Resolved: ":80", Gap: true}},
				Gap:         true,
			},
		},
		Services: []ServiceEnv{{
			Service:  "web",
			EnvFiles: []string{"/p/web/.web.env"},
			Entries: []EnvEntry{
				{Key: "WEB_PORT", Value: "18080", Source: Source{File: "/p/web/.web.env", Layer: "env_file"}},
				{Key: "WEB_DEBUG", Value: "true", Source: Source{File: "(inline environment:)", Layer: "environment"}},
			},
		}},
		Layers: []OverviewLayer{
			// Layer-1 chain: .env defines SITE_URL (+) and COMPOSE_ENV (+).
			{File: "/p/.env", Layer: "layer1", Entries: []OverviewEntry{
				{Key: "COMPOSE_ENV", RawValue: "dev"},
				{Key: "SITE_URL", RawValue: "example.com"},
			}},
			// Layer-1 chain: .dev.env overrides SITE_URL (~) and adds IS_DEV (+).
			{File: "/p/.dev.env", Layer: "layer1", Entries: []OverviewEntry{
				{Key: "IS_DEV", RawValue: "true"},
				{Key: "SITE_URL", RawValue: "dev.example.com"},
			}},
			// Runtime: web service .web.env adds WEB_PORT (+).
			{File: "/p/web/.web.env", Layer: "env_file", Service: "web", Entries: []OverviewEntry{
				{Key: "WEB_PORT", RawValue: "18080"},
			}},
			// Runtime: web service inline environment: adds WEB_DEBUG (+), overrides WEB_PORT (~).
			{File: "(inline environment:)", Layer: "environment", Service: "web", Entries: []OverviewEntry{
				{Key: "WEB_DEBUG", RawValue: "true"},
				{Key: "WEB_PORT", RawValue: "${WEB_PORT:-0}"},
			}},
		},
	}
}

// TestRenderHuman_Overview_Markers: +/~/· markers are correctly derived from the
// accumulator walk across layer1 files.
func TestRenderHuman_Overview_Markers(t *testing.T) {
	var b bytes.Buffer
	RenderHuman(&b, overviewReport(), HumanOpts{
		Overview:         true,
		ComposeEnv:       "dev",
		ComposeEnvSource: ".env",
		ProjectDir:       "/p",
	})
	got := b.String()

	// .env defines SITE_URL first → + marker
	if !strings.Contains(got, "+ SITE_URL") {
		t.Fatalf("overview: expected '+ SITE_URL' (first definition) in output:\n%s", got)
	}
	// .dev.env overrides SITE_URL → ~ marker with old→new
	if !strings.Contains(got, "~ SITE_URL") {
		t.Fatalf("overview: expected '~ SITE_URL' (override) in output:\n%s", got)
	}
	// old→new form: "example.com → dev.example.com"
	if !strings.Contains(got, "example.com") || !strings.Contains(got, "dev.example.com") {
		t.Fatalf("overview: expected old→new values for SITE_URL override:\n%s", got)
	}
	// IS_DEV first defined in .dev.env → + marker
	if !strings.Contains(got, "+ IS_DEV") {
		t.Fatalf("overview: expected '+ IS_DEV' in output:\n%s", got)
	}
	// must NOT show the trace-mode gap path (the interpolation:/runtime: prefixed lines)
	if strings.Contains(got, "interpolation: NOT in the Layer-1 chain") {
		t.Fatalf("overview: must not contain trace-mode 'interpolation:' gap text:\n%s", got)
	}
}

// TestRenderHuman_Overview_TwoSections: output has both the chain section header
// and the runtime-only section header; inline environment: appears LAST within a
// service's block.
func TestRenderHuman_Overview_TwoSections(t *testing.T) {
	var b bytes.Buffer
	RenderHuman(&b, overviewReport(), HumanOpts{
		Overview:   true,
		ProjectDir: "/p",
	})
	got := b.String()

	// chain section header
	if !strings.Contains(got, "Interpolation chain") {
		t.Fatalf("overview: missing 'Interpolation chain' section header:\n%s", got)
	}
	// runtime section header
	if !strings.Contains(got, "Runtime-only") {
		t.Fatalf("overview: missing 'Runtime-only' section header:\n%s", got)
	}
	// inline environment: layer renders within the service block
	if !strings.Contains(got, "inline environment:") {
		t.Fatalf("overview: missing 'inline environment:' layer heading:\n%s", got)
	}
	// inline environment: must appear AFTER .web.env in the output
	webEnvIdx := strings.Index(got, ".web.env")
	inlineIdx := strings.Index(got, "inline environment:")
	if webEnvIdx == -1 || inlineIdx == -1 || inlineIdx <= webEnvIdx {
		t.Fatalf("overview: 'inline environment:' must appear after .web.env (got webEnvIdx=%d inlineIdx=%d):\n%s",
			webEnvIdx, inlineIdx, got)
	}
}

// TestRenderHuman_Overview_GapLine: a var with Gap=true and a matching service
// effect produces a ⚠ gap: annotation after that service's block.
func TestRenderHuman_Overview_GapLine(t *testing.T) {
	var b bytes.Buffer
	RenderHuman(&b, overviewReport(), HumanOpts{
		Overview:   true,
		ProjectDir: "/p",
	})
	got := b.String()

	// WEB_PORT is gap — ⚠ gap line must appear
	if !strings.Contains(got, "gap") {
		t.Fatalf("overview: expected gap annotation for WEB_PORT:\n%s", got)
	}
	if !strings.Contains(got, "WEB_PORT") {
		t.Fatalf("overview: expected WEB_PORT in gap annotation:\n%s", got)
	}
}

// TestRenderHuman_Overview_Header: the header block carries COMPOSE_ENV=<v>
// (from <source>) and Project dir.
func TestRenderHuman_Overview_Header(t *testing.T) {
	var b bytes.Buffer
	RenderHuman(&b, overviewReport(), HumanOpts{
		Overview:         true,
		ComposeEnv:       "dev",
		ComposeEnvSource: ".env",
		ProjectDir:       "/p",
	})
	got := b.String()

	if !strings.Contains(got, "COMPOSE_ENV") || !strings.Contains(got, "dev") {
		t.Fatalf("overview: expected COMPOSE_ENV=dev in header:\n%s", got)
	}
	if !strings.Contains(got, ".env") {
		t.Fatalf("overview: expected source label '.env' in header:\n%s", got)
	}
	if !strings.Contains(got, "/p") {
		t.Fatalf("overview: expected ProjectDir '/p' in header:\n%s", got)
	}
}

// TestRenderJSON_Layers_Schema: RenderJSON includes the Layers structure under
// the "layers" key with the correct schema (file/layer/service/entries/key/raw_value).
func TestRenderJSON_Layers_Schema(t *testing.T) {
	var b bytes.Buffer
	if err := RenderJSON(&b, overviewReport()); err != nil {
		t.Fatal(err)
	}
	got := b.String()

	for _, want := range []string{
		`"layers"`,
		`"file"`,
		`"layer"`,
		`"entries"`,
		`"key"`,
		`"raw_value"`,
		`"layer1"`,
		`"env_file"`,
		`"environment"`,
		`"service"`,
		`"(inline environment:)"`,
		// literal value with ${...} must survive into JSON unmodified
		`"${WEB_PORT:-0}"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("JSON Layers schema missing %q:\n%s", want, got)
		}
	}
	// gate check: other modes' JSON must not be altered (no regression on existing fields)
	for _, existing := range []string{`"files"`, `"chain_files"`, `"vars"`, `"in_chain"`} {
		if !strings.Contains(got, existing) {
			t.Fatalf("JSON regression: existing field %q missing after Layers addition:\n%s", existing, got)
		}
	}
}

// TestRenderHuman_Overview_NotInDefaultMode: HumanOpts{} (default chain mode)
// must NOT invoke renderOverview even when Layers is populated.
func TestRenderHuman_Overview_NotInDefaultMode(t *testing.T) {
	var b bytes.Buffer
	// default opts (no Overview flag set) — must render chain list, not overview
	RenderHuman(&b, overviewReport(), HumanOpts{})
	got := b.String()
	// default mode emits chain files; must NOT show overview section headers
	if strings.Contains(got, "Interpolation chain") {
		t.Fatalf("default mode must not render overview section:\n%s", got)
	}
	if strings.Contains(got, "Runtime-only") {
		t.Fatalf("default mode must not render runtime-only section:\n%s", got)
	}
}

// ─── end --overview render tests ──────────────────────────────────────────────

// ─── Styler / color tests ─────────────────────────────────────────────────────
//
// These tests exercise the HumanOpts.Style injection path:
//   - nil Style (default, no field set) → plain output, byte-identical to pre-color
//   - ansiStyler (a forced-ANSI fake) → ANSI escapes appear around styled elements
//   - RenderJSON is NEVER colored regardless of the Style field
//
// ansiStyler is a test-only Styler implementation that wraps every call in a
// recognisable ESC marker so tests can assert ANSI presence without needing
// the real lipgloss renderer. It must match the Styler interface exactly:
// marker/arrow methods take no argument and return the styled glyph; all
// others take a string and return a styled string.
type ansiStyler struct{}

func (ansiStyler) Header(s string) string      { return "\x1b[H" + s + "\x1b[0m" }
func (ansiStyler) MarkerNew() string           { return "\x1b[N+\x1b[0m" }
func (ansiStyler) MarkerOverride() string      { return "\x1b[O~\x1b[0m" }
func (ansiStyler) MarkerRepeat() string        { return "\x1b[R·\x1b[0m" }
func (ansiStyler) Arrow() string               { return "\x1b[A→\x1b[0m" }
func (ansiStyler) Key(s string) string         { return "\x1b[K" + s + "\x1b[0m" }
func (ansiStyler) Value(s string) string       { return "\x1b[V" + s + "\x1b[0m" }
func (ansiStyler) Old(s string) string         { return "\x1b[D" + s + "\x1b[0m" }
func (ansiStyler) Path(s string) string        { return "\x1b[P" + s + "\x1b[0m" }
func (ansiStyler) Service(s string) string     { return "\x1b[S" + s + "\x1b[0m" }
func (ansiStyler) SourceLabel(s string) string { return "\x1b[L" + s + "\x1b[0m" }
func (ansiStyler) Gap(s string) string         { return "\x1b[G" + s + "\x1b[0m" }
func (ansiStyler) GapName(s string) string     { return "\x1b[GN" + s + "\x1b[0m" }
func (ansiStyler) Ok(s string) string          { return "\x1b[OK" + s + "\x1b[0m" }
func (ansiStyler) Fail(s string) string        { return "\x1b[F" + s + "\x1b[0m" }
func (ansiStyler) Created(s string) string     { return "\x1b[C" + s + "\x1b[0m" }
func (ansiStyler) Skipped(s string) string     { return "\x1b[SK" + s + "\x1b[0m" }
func (ansiStyler) ErrorMsg(s string) string    { return "\x1b[E" + s + "\x1b[0m" }

// compile-guard: ansiStyler must satisfy the Styler interface.
var _ Styler = ansiStyler{}

// TestRenderHuman_NilStyle_PlainOutput: existing HumanOpts with no Style set
// (nil) must produce byte-identical plain output — the nil-safe st() helper
// falls through to plainStyler{}. This is the zero-churn guarantee.
func TestRenderHuman_NilStyle_PlainOutput(t *testing.T) {
	var b bytes.Buffer
	// Use overview mode (the most style-heavy path) with no Style field.
	RenderHuman(&b, overviewReport(), HumanOpts{
		Overview:         true,
		ComposeEnv:       "dev",
		ComposeEnvSource: ".env",
		ProjectDir:       "/p",
	})
	got := b.String()
	if strings.Contains(got, "\x1b") {
		t.Fatalf("nil Style produced ANSI escapes — plain styler must be the default:\n%s", got)
	}
	// sanity: output is non-empty
	if strings.TrimSpace(got) == "" {
		t.Fatalf("nil Style produced empty overview output")
	}
}

// TestRenderHuman_ANSIStyle_OverviewMarkers: a forced-ANSI styler injects
// escape sequences around markers, the header, and the gap line in --overview.
func TestRenderHuman_ANSIStyle_OverviewMarkers(t *testing.T) {
	var b bytes.Buffer
	RenderHuman(&b, overviewReport(), HumanOpts{
		Overview:         true,
		ComposeEnv:       "dev",
		ComposeEnvSource: ".env",
		ProjectDir:       "/p",
		Style:            ansiStyler{},
	})
	got := b.String()

	// ANSI must appear somewhere (basic guard)
	if !strings.Contains(got, "\x1b") {
		t.Fatalf("forced-ANSI Styler produced no escapes in --overview output:\n%s", got)
	}
	// The section header must be styled (wrapped by Header())
	if !strings.Contains(got, "\x1b[H") {
		t.Fatalf("forced-ANSI: header section not styled (no \\x1b[H prefix):\n%s", got)
	}
	// A marker (+ or ~) must carry ANSI (MarkerNew wraps with \x1b[N)
	if !strings.Contains(got, "\x1b[N") && !strings.Contains(got, "\x1b[O") {
		t.Fatalf("forced-ANSI: no marker escape (\\x1b[N or \\x1b[O) in --overview output:\n%s", got)
	}
	// The gap line must be styled (Gap() wraps with \x1b[G)
	if !strings.Contains(got, "\x1b[G") {
		t.Fatalf("forced-ANSI: gap line not styled (no \\x1b[G prefix):\n%s", got)
	}
}

// TestRenderJSON_NoANSIEvenWithStyle: RenderJSON must never contain ANSI
// escape sequences regardless of what Styler is in play (JSON is machine
// output; the --json path always uses Disabled — this guards against any
// accidental wiring of the human styler into RenderJSON).
//
// Simulated by building a Report that includes Layers and calling RenderJSON
// — if the serializer ever touched the Styler, the output would contain \x1b.
// RenderJSON does not take HumanOpts at all (it serializes the raw Report), so
// this test is a regression guard against a future refactor wiring them.
func TestRenderJSON_NoANSIEvenWithStyle(t *testing.T) {
	var b bytes.Buffer
	if err := RenderJSON(&b, overviewReport()); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if strings.Contains(got, "\x1b") {
		t.Fatalf("RenderJSON output contains ANSI escape — JSON must always be plain:\n%s", got)
	}
}

// TestRenderHuman_ANSIStyle_TraceInChain: forced-ANSI styler applies to the
// --trace (in-chain) path: winner file should be styled as Path().
func TestRenderHuman_ANSIStyle_TraceInChain(t *testing.T) {
	var b bytes.Buffer
	RenderHuman(&b, sampleReport(), HumanOpts{
		Trace: "APP_PORT",
		Style: ansiStyler{},
	})
	got := b.String()
	if !strings.Contains(got, "\x1b") {
		t.Fatalf("forced-ANSI Styler produced no escapes in --trace output:\n%s", got)
	}
}

// TestRenderHuman_NilStyle_TraceUnchanged: nil Style on --trace must give the
// same plain text as before the color feature (no-churn guard on trace path).
func TestRenderHuman_NilStyle_TraceUnchanged(t *testing.T) {
	var withNil bytes.Buffer
	RenderHuman(&withNil, sampleReport(), HumanOpts{Trace: "APP_PORT"})
	got := withNil.String()
	if strings.Contains(got, "\x1b") {
		t.Fatalf("nil Style --trace produced ANSI — must be plain:\n%s", got)
	}
	for _, want := range []string{"APP_PORT=8080", ".env", "ports[0]", "8080:80"} {
		if !strings.Contains(got, want) {
			t.Fatalf("nil Style --trace missing %q (regression):\n%s", want, got)
		}
	}
}

// ─── end Styler / color tests ─────────────────────────────────────────────────

// TestRenderFiles_FullyOverriddenEnvFileStillListed: a service whose every
// env_file: key is shadowed by an inline environment: entry must STILL appear
// in the --files runtime-only group (N-3 guard).
//
// Pre-fix: renderFiles derived the list from per-key Entry sources; a file
// whose every key resolved to source.Layer="environment" would be absent.
// Post-fix: renderFiles uses ServiceEnv.EnvFiles (declared paths) — the file
// appears regardless of per-key overrides.
//
// RED on the pre-fix impl (entries-derived list → x service absent).
// GREEN on the current impl (EnvFiles-derived list → x service present).
func TestRenderFiles_FullyOverriddenEnvFileStillListed(t *testing.T) {
	rep := Report{
		Files:      []string{"/p/.env"},
		ChainFiles: []string{"/p/.env"},
		Services: []ServiceEnv{{
			Service: "x",
			// Every key comes from inline environment:, NOT from the env_file.
			Entries: []EnvEntry{
				{Key: "K", Value: "override", Source: Source{File: "(inline environment:)", Layer: "environment"}},
			},
			// The declared env_file: path — must appear in runtime-only even though
			// all its keys are overridden at the Entries level.
			EnvFiles: []string{"/p/x/.x.env"},
		}},
	}
	var b bytes.Buffer
	RenderHuman(&b, rep, HumanOpts{Files: true})
	got := b.String()
	// runtime-only group must list the file
	if !strings.Contains(got, "/p/x/.x.env") {
		t.Fatalf("--files runtime-only must include fully-overridden env_file /p/x/.x.env:\n%s", got)
	}
	// and it must be under the correct group heading
	if !strings.Contains(got, "runtime-only") {
		t.Fatalf("--files output missing runtime-only group heading:\n%s", got)
	}
}
