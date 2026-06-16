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
// Fixture: examples/monorepo/ with web/.web.env, api/.api.env confirmed on disk.
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
	// web/.web.env and api/.api.env are confirmed present on disk in examples/monorepo.
	for _, want := range []string{".web.env", ".api.env"} {
		if !got[want] {
			t.Fatalf("expected %s in EnvFiles; got %v", want, res.EnvFiles)
		}
	}
}

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
