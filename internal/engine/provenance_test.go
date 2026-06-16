package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/compose-envkit/compose-envkit/internal/engine"
	"github.com/compose-envkit/compose-envkit/internal/provenance"
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
	webSvc := findService(rep, "web")
	if got := entry(webSvc, "TIER"); got.Value != "staging" || got.Source.Layer != "environment" {
		t.Fatalf("TIER should be inline 'staging', got %+v", got)
	}
	if got := entry(webSvc, "WEB_ONLY"); got.Value != "yes" || got.Source.Layer != "env_file" {
		t.Fatalf("WEB_ONLY should be env_file 'yes', got %+v", got)
	}
	// A: WEB_ONLY winner is web.env
	if rep.Vars["WEB_ONLY"].Winner.File != web {
		t.Fatalf("WEB_ONLY winner = %q, want %q", rep.Vars["WEB_ONLY"].Winner.File, web)
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
