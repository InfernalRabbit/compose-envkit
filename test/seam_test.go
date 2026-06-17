// Package acceptance — white-box seam test.
//
// D4 (spec §8): TWO contracts tested here:
//
//  1. Engine-enumeration contract: engine.Resolve() still enumerates the active
//     Layer-2 set (er.EnvFiles contains service env_file: paths). The gap-detector
//     depends on this enumeration. This contract is UNCHANGED in v3.
//
//  2. Run-path L1-only contract (v3 new): the cmd-level run path builds
//     COMPOSE_ENV_FILES from Layer-1 only (er.EnvFiles are NOT appended). This is
//     the v3 behavior change; the test is RED on a pre-v3 (L2-appending) impl.
package acceptance

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/InfernalRabbit/compose-envkit/internal/chain"
	"github.com/InfernalRabbit/compose-envkit/internal/engine"
	"github.com/InfernalRabbit/compose-envkit/internal/envfiles"
)

// TestSeam_ChainToEngine_EnumerationContract (D4 contract 1) feeds chain.Resolve()
// output directly into engine.Resolve() via cr.Vars (the hand-off contract) and
// asserts that engine.Resolve() still enumerates the active Layer-2 set in
// er.EnvFiles. The gap-detector (env-debug) depends on this. Asserts:
//   - er.EnvFiles is non-empty (engine enumerates Layer-2)
//   - Layer-2 service files (.web.env, .api.env, .reports.env) are in er.EnvFiles
//   - All Layer-2 paths are absolute
//   - COMPOSE_ENV in cr.Vars is "dev" (chain→engine hand-off seam)
func TestSeam_ChainToEngine_EnumerationContract(t *testing.T) {
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

	// Engine must still enumerate Layer-2 (gap-detector depends on this)
	if len(er.EnvFiles) == 0 {
		t.Fatal("engine enumeration contract: er.EnvFiles must be non-empty (Layer-2 set needed for gap-detector)")
	}

	// All Layer-2 paths must be absolute
	for _, p := range er.EnvFiles {
		if !filepath.IsAbs(p) {
			t.Fatalf("non-absolute path in er.EnvFiles: %q", p)
		}
	}

	// All known service env_file: paths must appear in er.EnvFiles
	l2Map := map[string]bool{}
	for _, p := range er.EnvFiles {
		l2Map[filepath.Base(p)] = true
	}
	for _, want := range []string{".web.env", ".api.env", ".reports.env"} {
		if !l2Map[want] {
			t.Fatalf("engine enumeration contract: expected %s in er.EnvFiles; got %v", want, er.EnvFiles)
		}
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

// TestSeam_RunPath_L1Only (D4 contract 2 — v3 new, RED on pre-v3 impl) asserts
// that the run-path COMPOSE_ENV_FILES is built from Layer-1 only.
// Contract: envfiles.Assemble(cr.Files, nil) — NO er.EnvFiles appended.
// The resulting list must NOT contain any service env_file: path.
func TestSeam_RunPath_L1Only(t *testing.T) {
	root := stageMonorepo(t)

	cr, err := chain.Resolve(chain.Input{
		ProjectDir: root,
		OSEnv:      []string{"COMPOSE_ENV=dev", "HOSTNAME=testhost"},
		Hostname:   func() (string, error) { return "testhost", nil },
	})
	if err != nil {
		t.Fatalf("chain.Resolve: %v", err)
	}

	// v3 run path: assemble with nil Layer-2 (no service env_file: injection)
	runList, err := envfiles.Assemble(cr.Files, nil)
	if err != nil {
		t.Fatalf("envfiles.Assemble (L1-only): %v", err)
	}

	// Run list must be non-empty (Layer-1 chain files are present)
	if len(runList) == 0 {
		t.Fatal("run-path L1-only: COMPOSE_ENV_FILES must not be empty (Layer-1 files expected)")
	}

	// All paths must be absolute
	for _, p := range runList {
		if !filepath.IsAbs(p) {
			t.Fatalf("non-absolute path in run list: %q", p)
		}
	}

	// No service env_file: path must appear in the run list
	runMap := map[string]bool{}
	for _, p := range runList {
		runMap[filepath.Base(p)] = true
	}
	for _, absent := range []string{".web.env", ".api.env", ".reports.env"} {
		if runMap[absent] {
			t.Fatalf("run-path L1-only contract violated: %s must NOT appear in COMPOSE_ENV_FILES", absent)
		}
	}

	// The Layer-1 root .env must be present (sanity)
	if !runMap[".env"] {
		t.Fatalf("run-path L1-only: .env must appear in COMPOSE_ENV_FILES; got %v", runList)
	}
}
