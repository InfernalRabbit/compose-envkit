package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/InfernalRabbit/compose-envkit/internal/engine"
	"github.com/InfernalRabbit/compose-envkit/internal/provenance"
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

// findService returns the ServiceEnv for name; fatals if absent.
func findService(rep provenance.Report, name string) provenance.ServiceEnv {
	for _, se := range rep.Services {
		if se.Service == name {
			return se
		}
	}
	panic("service not found in report: " + name)
}

// entry returns the EnvEntry for key in se; fatals if absent.
func entry(se provenance.ServiceEnv, key string) provenance.EnvEntry {
	for _, e := range se.Entries {
		if e.Key == key {
			return e
		}
	}
	panic("key not found in service " + se.Service + ": " + key)
}

func TestProvenance_BLite_And_C(t *testing.T) {
	dir := t.TempDir()
	writeF(t, dir, "compose.yaml", `
services:
  web:
    image: busybox
    ports:
      - "${WEB_PORT}:80"
    environment:
      TIER: "${COMPOSE_ENV}"
    env_file:
      - ./web.env
`)
	// WEB_PORT lives ONLY in the Layer-2 ProvFile (NOT in env slice / Layer-1).
	// Under v3 the interpolation mapping is Layer-1-only, so WEB_PORT is a gap var:
	// ${WEB_PORT} falls back, Effect.Resolved is the fallback, Gap=true.
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

	// B-lite (v3 INVERT — env_file-only var falls back at interpolation; gap flagged):
	// WEB_PORT lives ONLY in web.env (Layer-2 ProvFile) and is NOT in the env slice.
	// Under v3 the interpolation mapping is Layer-1-only, so ${WEB_PORT} (no default)
	// resolves to the blank/fallback ":80", NOT "8080:80". Gap=true because the var
	// is referenced in a service field and IS in a service env_file but NOT in the
	// interpolation env — exactly the #3435 footprint.
	wp := rep.Vars["WEB_PORT"]
	if len(wp.Effects) == 0 || wp.Effects[0].Service != "web" || wp.Effects[0].Resolved != ":80" {
		t.Fatalf("WEB_PORT effect wrong (want Resolved=:80 fallback): %+v", wp.Effects)
	}
	if !wp.Effects[0].Gap {
		t.Fatalf("WEB_PORT Effect.Gap = false, want true (env_file-only var)")
	}
	if wp.InChain {
		t.Fatalf("WEB_PORT InChain = true, want false (not in Layer-1 env)")
	}
	if wp.Gap != true {
		t.Fatalf("WEB_PORT Gap = false, want true")
	}
	if len(wp.RuntimeDefs) == 0 || wp.RuntimeDefs[0].Service != "web" || wp.RuntimeDefs[0].Value != "8080" {
		t.Fatalf("WEB_PORT RuntimeDefs wrong (want web/8080): %+v", wp.RuntimeDefs)
	}
	// C: inline environment TIER overrides env_file TIER; WEB_ONLY from env_file
	webSvc := findService(rep, "web")
	if got := entry(webSvc, "TIER"); got.Value != "staging" || got.Source.Layer != "environment" {
		t.Fatalf("TIER should be inline 'staging', got %+v", got)
	}
	if got := entry(webSvc, "WEB_ONLY"); got.Value != "yes" || got.Source.Layer != "env_file" {
		t.Fatalf("WEB_ONLY should be env_file 'yes', got %+v", got)
	}
	// A (v3): WEB_ONLY is in a Layer-2 ProvFile and is NOT referenced in any service
	// field, so it has no VarTrace entry at all — A-attribution covers Layer-1 only,
	// and B-lite only creates entries for vars that appear in service field templates.
	// Its value is accessible only via C (ServiceEnv), which is asserted above.
	if _, ok := rep.Vars["WEB_ONLY"]; ok {
		t.Fatalf("WEB_ONLY must NOT appear in rep.Vars under v3 (Layer-2, not field-referenced); got %+v", rep.Vars["WEB_ONLY"])
	}
}

// TestProvenance_ChainOnly: a dir with no compose file → Provenance returns a
// Report with Vars (A) populated and empty Services/Effects, no error.
func TestProvenance_ChainOnly(t *testing.T) {
	dir := t.TempDir()
	// No compose file — chain-only mode.
	chainFile := writeF(t, dir, "chain.env", "CHAIN_VAR=hello\n")

	rep, err := engine.New().Provenance(context.Background(), engine.ProvInput{
		ProjectDir: dir,
		Env:        []string{},
		EnvFiles:   []engine.ProvFile{{Path: chainFile, Layer: "layer1"}},
	})
	if err != nil {
		t.Fatalf("Provenance chain-only: %v", err)
	}
	// A: chain vars populated
	if rep.Vars["CHAIN_VAR"].Value != "hello" {
		t.Fatalf("CHAIN_VAR not found in Vars: %+v", rep.Vars)
	}
	// C: no compose file → Services must be empty
	if len(rep.Services) != 0 {
		t.Fatalf("chain-only mode must have empty Services, got %+v", rep.Services)
	}
	// B-lite: no compose model to walk → no Effects
	for k, vt := range rep.Vars {
		if len(vt.Effects) != 0 {
			t.Fatalf("chain-only var %q must have no Effects, got %+v", k, vt.Effects)
		}
	}
}
