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

// ─── WantLayers tests ────────────────────────────────────────────────────────

// TestProvenance_WantLayers_GateOff: when WantLayers=false (default), rep.Layers
// is nil/empty regardless of whether chain files or compose services exist.
// This is the D-A gate: existing modes' --json output must remain byte-identical.
func TestProvenance_WantLayers_GateOff(t *testing.T) {
	dir := t.TempDir()
	envFile := writeF(t, dir, "chain.env", "SITE_URL=example.com\nCOMPOSE_ENV=dev\n")
	writeF(t, dir, "compose.yaml", `
services:
  web:
    image: busybox
    env_file:
      - path: ./svc.env
        required: false
    environment:
      WEB_DEBUG: "true"
`)
	writeF(t, dir, "svc.env", "WEB_PORT=18080\n")

	rep, err := engine.New().Provenance(context.Background(), engine.ProvInput{
		ProjectDir: dir,
		Env:        []string{"COMPOSE_ENV=dev"},
		EnvFiles:   []engine.ProvFile{{Path: envFile, Layer: "layer1"}},
		WantLayers: false, // explicit default
	})
	if err != nil {
		t.Fatalf("Provenance WantLayers=false: %v", err)
	}
	// D-A gate: Layers must be empty when not requested
	if len(rep.Layers) != 0 {
		t.Fatalf("WantLayers=false: Layers must be nil/empty, got %d entries: %+v", len(rep.Layers), rep.Layers)
	}
}

// TestProvenance_WantLayers_OrderedLiteral: when WantLayers=true, rep.Layers is
// populated in chain order (layer1 files first), then per-service (env_file then
// inline-environment), with LITERAL values — ${...} expressions are NOT expanded.
// This is the key regression guard: dotenv would have expanded them.
func TestProvenance_WantLayers_OrderedLiteral(t *testing.T) {
	dir := t.TempDir()
	// chain: .env defines two vars; one has a ${...} interpolation reference
	// that dotenv WOULD expand — we must see it literal.
	envFile := writeF(t, dir, ".env", "SITE_URL=example.com\nPOSTGRES_USER=${DATABASE_POSTGRES_USER:-directus}\n")
	// chain: .dev.env overrides SITE_URL and adds IS_DEV
	devEnvFile := writeF(t, dir, ".dev.env", "IS_DEV=true\nSITE_URL=dev.example.com\n")
	// service env_file: defines WEB_PORT (also with ${...} to guard literal capture)
	svcEnvFile := writeF(t, dir, "svc.env", "WEB_PORT=${WEB_PORT_OVERRIDE:-18080}\n")
	writeF(t, dir, "compose.yaml", `
services:
  web:
    image: busybox
    env_file:
      - path: ./svc.env
        required: false
    environment:
      WEB_DEBUG: "true"
`)

	rep, err := engine.New().Provenance(context.Background(), engine.ProvInput{
		ProjectDir: dir,
		Env:        []string{"COMPOSE_ENV=dev"},
		EnvFiles: []engine.ProvFile{
			{Path: envFile, Layer: "layer1"},
			{Path: devEnvFile, Layer: "layer1"},
		},
		WantLayers: true,
	})
	if err != nil {
		t.Fatalf("Provenance WantLayers=true: %v", err)
	}

	// must have at least 3 layers: .env, .dev.env, svc.env (+ possibly inline env)
	if len(rep.Layers) < 3 {
		t.Fatalf("WantLayers=true: expected >=3 layers, got %d: %+v", len(rep.Layers), rep.Layers)
	}

	// layer order: chain files (layer1) BEFORE service layers
	var firstChainIdx, firstSvcIdx int = -1, -1
	for i, l := range rep.Layers {
		if l.Layer == "layer1" && firstChainIdx == -1 {
			firstChainIdx = i
		}
		if (l.Layer == "env_file" || l.Layer == "environment") && firstSvcIdx == -1 {
			firstSvcIdx = i
		}
	}
	if firstChainIdx == -1 {
		t.Fatalf("WantLayers: no layer1 layers found in %+v", rep.Layers)
	}
	if firstSvcIdx != -1 && firstChainIdx >= firstSvcIdx {
		t.Fatalf("WantLayers: chain (layer1) layers must come before service layers (chain=%d svc=%d)", firstChainIdx, firstSvcIdx)
	}

	// LITERAL-VALUE GUARD: the regression guard for dotenv expansion.
	// Find the .env layer and assert POSTGRES_USER has the literal ${...} expression.
	var envLayer *provenance.OverviewLayer
	for i := range rep.Layers {
		if rep.Layers[i].File == envFile && rep.Layers[i].Layer == "layer1" {
			envLayer = &rep.Layers[i]
			break
		}
	}
	if envLayer == nil {
		t.Fatalf("WantLayers: .env layer1 layer not found in %+v", rep.Layers)
	}
	var postgresEntry *provenance.OverviewEntry
	for i := range envLayer.Entries {
		if envLayer.Entries[i].Key == "POSTGRES_USER" {
			postgresEntry = &envLayer.Entries[i]
			break
		}
	}
	if postgresEntry == nil {
		t.Fatalf("WantLayers: POSTGRES_USER not in .env layer entries: %+v", envLayer.Entries)
	}
	// THE KEY ASSERTION: must NOT be expanded by dotenv ("directus"); must be literal.
	if postgresEntry.RawValue == "directus" {
		t.Fatalf("WantLayers: POSTGRES_USER RawValue=%q was EXPANDED by dotenv; must be literal ${DATABASE_POSTGRES_USER:-directus}", postgresEntry.RawValue)
	}
	if postgresEntry.RawValue != "${DATABASE_POSTGRES_USER:-directus}" {
		t.Fatalf("WantLayers: POSTGRES_USER RawValue=%q, want literal ${DATABASE_POSTGRES_USER:-directus}", postgresEntry.RawValue)
	}

	// LITERAL-VALUE GUARD for service env_file: WEB_PORT must also be literal.
	var svcLayer *provenance.OverviewLayer
	for i := range rep.Layers {
		if rep.Layers[i].File == svcEnvFile && rep.Layers[i].Layer == "env_file" {
			svcLayer = &rep.Layers[i]
			break
		}
	}
	if svcLayer == nil {
		t.Fatalf("WantLayers: svc.env env_file layer not found in %+v", rep.Layers)
	}
	var webPortEntry *provenance.OverviewEntry
	for i := range svcLayer.Entries {
		if svcLayer.Entries[i].Key == "WEB_PORT" {
			webPortEntry = &svcLayer.Entries[i]
			break
		}
	}
	if webPortEntry == nil {
		t.Fatalf("WantLayers: WEB_PORT not in svc.env layer entries: %+v", svcLayer.Entries)
	}
	if webPortEntry.RawValue == "18080" {
		t.Fatalf("WantLayers: WEB_PORT RawValue=%q was expanded; must be literal ${WEB_PORT_OVERRIDE:-18080}", webPortEntry.RawValue)
	}
	if webPortEntry.RawValue != "${WEB_PORT_OVERRIDE:-18080}" {
		t.Fatalf("WantLayers: WEB_PORT RawValue=%q, want literal ${WEB_PORT_OVERRIDE:-18080}", webPortEntry.RawValue)
	}

	// inline environment: layer must appear LAST within the service's block
	// (after env_file layers for the same service)
	webSvcEnvFileIdx, webInlineIdx := -1, -1
	for i, l := range rep.Layers {
		if l.Service == "web" {
			if l.Layer == "env_file" {
				webSvcEnvFileIdx = i
			}
			if l.Layer == "environment" {
				webInlineIdx = i
			}
		}
	}
	if webInlineIdx != -1 && webSvcEnvFileIdx != -1 && webInlineIdx <= webSvcEnvFileIdx {
		t.Fatalf("WantLayers: inline environment: layer (idx=%d) must come AFTER env_file layer (idx=%d) for service web",
			webInlineIdx, webSvcEnvFileIdx)
	}
}

// TestProvenance_WantLayers_DeclarationOrder: entries within a layer are in
// declaration order (the order they appear in the file), not sorted.
// Uses a file with keys intentionally in reverse-alphabetical order.
func TestProvenance_WantLayers_DeclarationOrder(t *testing.T) {
	dir := t.TempDir()
	// Z_FIRST before A_SECOND — declaration order must be preserved.
	envFile := writeF(t, dir, ".env", "Z_FIRST=1\nA_SECOND=2\nM_THIRD=3\n")

	rep, err := engine.New().Provenance(context.Background(), engine.ProvInput{
		ProjectDir: dir,
		Env:        []string{},
		EnvFiles:   []engine.ProvFile{{Path: envFile, Layer: "layer1"}},
		WantLayers: true,
	})
	if err != nil {
		t.Fatalf("Provenance WantLayers declaration order: %v", err)
	}
	var layer *provenance.OverviewLayer
	for i := range rep.Layers {
		if rep.Layers[i].File == envFile {
			layer = &rep.Layers[i]
			break
		}
	}
	if layer == nil {
		t.Fatalf("WantLayers: .env layer not found")
	}
	if len(layer.Entries) != 3 {
		t.Fatalf("WantLayers declaration order: expected 3 entries, got %d: %+v", len(layer.Entries), layer.Entries)
	}
	if layer.Entries[0].Key != "Z_FIRST" {
		t.Fatalf("WantLayers declaration order: Entries[0].Key=%q, want Z_FIRST (declaration order, not sorted)", layer.Entries[0].Key)
	}
	if layer.Entries[1].Key != "A_SECOND" {
		t.Fatalf("WantLayers declaration order: Entries[1].Key=%q, want A_SECOND", layer.Entries[1].Key)
	}
}

// ─── end WantLayers tests ──────────────────────────────────────────────────────

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
