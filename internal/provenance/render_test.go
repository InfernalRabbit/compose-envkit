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
