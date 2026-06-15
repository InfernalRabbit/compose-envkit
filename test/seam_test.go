// Package acceptance — white-box seam test.
// Feeds chain.Resolve() output directly into engine.Resolve() and asserts
// the merged COMPOSE_ENV_FILES ordering end-to-end (acceptance-port-plan §3).
// This test catches contract drift between the layers that green unit tests on
// each side individually cannot detect.
package acceptance

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/compose-envkit/compose-envkit/internal/chain"
	"github.com/compose-envkit/compose-envkit/internal/engine"
	"github.com/compose-envkit/compose-envkit/internal/envfiles"
)

// TestSeam_ChainToEngine feeds chain.Resolve() output directly into
// engine.Resolve() via cr.Vars (the hand-off contract) and asserts:
//   - Layer-1 files appear before Layer-2 files in the merged list
//   - Layer-2 files (.web.env, .api.env) are present
//   - No duplicates
//   - All paths are absolute
func TestSeam_ChainToEngine(t *testing.T) {
	root := stageMonorepo(t)

	// Step 1: Layer-1 chain (pure Go, no compose-go)
	cr, err := chain.Resolve(chain.Input{
		ProjectDir: root,
		OSEnv:      []string{"COMPOSE_ENV=dev", "HOSTNAME=testhost"},
		Hostname:   func() (string, error) { return "testhost", nil },
	})
	if err != nil {
		t.Fatalf("chain.Resolve: %v", err)
	}

	// Step 2: Layer-2 engine seeded with chain output (cr.Vars is the hand-off)
	er, err := engine.New().Resolve(context.Background(), engine.Input{
		ProjectDir: root,
		Env:        cr.Vars, // THIS is the seam: chain output → engine input
	})
	if err != nil {
		t.Fatalf("engine.Resolve: %v", err)
	}

	// Step 3: assemble into COMPOSE_ENV_FILES
	merged, err := envfiles.Assemble(cr.Files, er.EnvFiles)
	if err != nil {
		t.Fatalf("envfiles.Assemble: %v", err)
	}

	if len(merged) == 0 {
		t.Fatal("merged COMPOSE_ENV_FILES must not be empty")
	}

	// All paths must be absolute
	for _, p := range merged {
		if !filepath.IsAbs(p) {
			t.Fatalf("non-absolute path in merged list: %q", p)
		}
	}

	// Layer-2 service env files must be present
	gotMap := map[string]int{}
	for i, p := range merged {
		gotMap[filepath.Base(p)] = i
	}
	for _, want := range []string{".web.env", ".api.env", ".reports.env"} {
		if _, ok := gotMap[want]; !ok {
			t.Fatalf("expected %s in merged COMPOSE_ENV_FILES; got %v", want, merged)
		}
	}

	// Layer-1 files must appear before Layer-2 files (the ordering contract)
	// .env is Layer-1; .web.env is Layer-2
	if idx1, ok1 := gotMap[".env"]; ok1 {
		if idx2, ok2 := gotMap[".web.env"]; ok2 {
			if idx1 >= idx2 {
				t.Fatalf("Layer-1 .env (pos %d) must precede Layer-2 .web.env (pos %d)", idx1, idx2)
			}
		}
	}
	if idx1, ok1 := gotMap[".dev.env"]; ok1 {
		if idx2, ok2 := gotMap[".api.env"]; ok2 {
			if idx1 >= idx2 {
				t.Fatalf("Layer-1 .dev.env (pos %d) must precede Layer-2 .api.env (pos %d)", idx1, idx2)
			}
		}
	}

	// No duplicates
	seen := map[string]bool{}
	for _, p := range merged {
		if seen[p] {
			t.Fatalf("duplicate path in merged COMPOSE_ENV_FILES: %q", p)
		}
		seen[p] = true
	}

	// COMPOSE_ENV in cr.Vars must be "dev" (chain resolved it correctly)
	composeEnvInVars := false
	for _, kv := range cr.Vars {
		if strings.HasPrefix(kv, "COMPOSE_ENV=") {
			if kv != "COMPOSE_ENV=dev" {
				t.Fatalf("cr.Vars COMPOSE_ENV=%q want dev", strings.TrimPrefix(kv, "COMPOSE_ENV="))
			}
			composeEnvInVars = true
		}
	}
	if !composeEnvInVars {
		t.Fatal("COMPOSE_ENV not found in cr.Vars (seam: chain must emit it)")
	}
}
