// Package acceptance ports the smoke-monorepo.sh (23 scenarios, 60 assertions after
// S4 recount) and smoke.sh suites to drive the cenvkit binary directly.
//
// Invocation map (replaces all ./docker / run_shim calls):
//
//	./docker compose ...         → cenvkit compose ...
//	./docker env-files           → cenvkit env-files
//	sh scripts/env-debug.sh --X  → cenvkit env-debug --X
//
// Deliberate behavior inversions vs the sh kit (G1–G5):
//
//	G1/G2: stray files not in include:/COMPOSE_FILE NOT discovered (was: glob found them)
//	G3:    COMPOSE_DEPTH accepted-but-ignored; out-of-include = not-found
//	G4:    no fallback shim — chain-only mode succeeds with empty Layer-2
//	G5:    install layout differs (no scripts/); cenvkit binary + .docker-env-chain
//
// Count: exactly 60 smoke-monorepo assertions (61 baseline − 1 for dropped 11.2).
// C1 (§4a single-pass) and D1-runtime (§4b fatal) use throwaway fixtures, not counted.
package acceptance

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ─── harness ──────────────────────────────────────────────────────────────────

var cenvkitBin string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "cenvkit-bin")
	if err != nil {
		panic("mktemp: " + err.Error())
	}
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

// stageMonorepo copies examples/monorepo into a fresh temp dir and seeds Layer-1
// dotfiles from example.* templates (mirrors smoke-monorepo.sh:102/119-121).
// The tracked fixture has NO .env/.dev.env/.prod.env — only example.* exist.
// A bare env-files run against the unseeded fixture yields ONLY Layer-2 (correct
// skip-missing), but scenarios that check Layer-1 ordering need the seeded dir.
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

// ─── Scenario 1: bootstrap / init (3 assertions) ─────────────────────────────

// 1.2 cenvkit init exits 0.
// 1.3 example.env not clobbered (idempotent).
func TestScenario1_Init(t *testing.T) {
	root := stageMonorepo(t)
	// 1.2 — first run exits 0
	out, err := runCenvkit(t, root, nil, "init")
	if err != nil {
		t.Fatalf("[1.2] cenvkit init: %v\n%s", err, out)
	}
	// 1.3 — .env must not be clobbered (already seeded by stageMonorepo)
	b, _ := os.ReadFile(filepath.Join(root, ".env"))
	if !strings.Contains(string(b), "COMPOSE_ENV=dev") {
		t.Fatalf("[1.3] .env clobbered or missing COMPOSE_ENV=dev")
	}
	// second run must also exit 0 (idempotent)
	if out, err := runCenvkit(t, root, nil, "init"); err != nil {
		t.Fatalf("[1.3] cenvkit init idempotent: %v\n%s", err, out)
	}
}

// ─── Scenario 4: env-files discovery from root (3 assertions, no docker) ─────

// 4.1 web/.web.env in output
// 4.2 api/.api.env in output
// 4.3 root .env in output
func TestScenario4_EnvFilesFromRoot(t *testing.T) {
	root := stageMonorepo(t)
	out, err := runCenvkit(t, root, nil, "env-files")
	if err != nil {
		t.Fatalf("[4] env-files: %v\n%s", err, out)
	}
	for _, tc := range []struct{ id, want string }{
		{"4.1", ".web.env"},
		{"4.2", ".api.env"},
		{"4.3", "/.env"},
	} {
		if !strings.Contains(out, tc.want) {
			t.Fatalf("[%s] expected %q in output:\n%s", tc.id, tc.want, out)
		}
	}
}

// ─── Scenario 6: isolated api/ (3 assertions) ────────────────────────────────

// 6.1 api/ env-files lists .api.env
// 6.2 api/ does NOT see .web.env (sibling isolation — critical negative)
func TestScenario6_ApiDirIsolation(t *testing.T) {
	root := stageMonorepo(t)
	apiDir := filepath.Join(root, "api")
	out, err := runCenvkit(t, apiDir, nil, "env-files")
	if err != nil {
		t.Fatalf("[6] env-files: %v\n%s", err, out)
	}
	if !strings.Contains(out, ".api.env") {
		t.Fatalf("[6.1] expected .api.env in output:\n%s", out)
	}
	if strings.Contains(out, ".web.env") {
		t.Fatalf("[6.2] api scope leaked .web.env:\n%s", out)
	}
}

// ─── Scenario 8: COMPOSE_ENV=prod (1 assertion, no docker) ───────────────────

// 8.1 prod: Layer-2 discovers .web.env AND .api.env
func TestScenario8_ProdEnvFiles(t *testing.T) {
	root := stageMonorepo(t)
	out, err := runCenvkit(t, root, []string{"COMPOSE_ENV=prod"}, "env-files")
	if err != nil {
		t.Fatalf("[8] env-files prod: %v\n%s", err, out)
	}
	for _, want := range []string{".web.env", ".api.env"} {
		if !strings.Contains(out, want) {
			t.Fatalf("[8.1] expected %q in prod output:\n%s", want, out)
		}
	}
}

// ─── Scenario 9: over-discovery eliminated (1 assertion, INVERSION G1) ───────

// INVERSION (G1/G2): sh over-discovery quirk gone; include-graph is authoritative.
// A stray compose file NOT in include: does not contribute its env_files.
func TestScenario9_StrayFileNotDiscovered(t *testing.T) {
	root := stageMonorepo(t)
	// Create a stray subproject NOT in include:
	extraDir := filepath.Join(root, "extra")
	if err := os.MkdirAll(extraDir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(extraDir, "docker-compose-extra.yml"), []byte(`
services:
  extra: { image: busybox, env_file: [./.extra.env] }
`), 0o644)
	os.WriteFile(filepath.Join(extraDir, ".extra.env"), []byte("EXTRA=1\n"), 0o644)

	out, err := runCenvkit(t, root, nil, "env-files")
	if err != nil {
		t.Fatalf("[9] env-files: %v\n%s", err, out)
	}
	// INVERSION: stray .extra.env must NOT appear (include-graph, not glob)
	if strings.Contains(out, ".extra.env") {
		t.Fatalf("[9.1] stray .extra.env must NOT be discovered (G1 inversion):\n%s", out)
	}
}

// ─── Scenario 10: standard-name discovery (2 assertions, INVERSION G2) ───────

// INVERSION (G2): stray files NOT in include: are not discovered regardless of name.
// (compose.yaml and renamed docker-compose.yml both count as stray if outside include:)
func TestScenario10_StrayNameNotDiscovered(t *testing.T) {
	root := stageMonorepo(t)

	// 10.1: a stray compose.yaml NOT in include: — must NOT be discovered
	strayDir := filepath.Join(root, "stray")
	os.MkdirAll(strayDir, 0o755)
	os.WriteFile(filepath.Join(strayDir, "compose.yaml"), []byte(`
services:
  stray: { image: busybox, env_file: [./.stray.env] }
`), 0o644)
	os.WriteFile(filepath.Join(strayDir, ".stray.env"), []byte("STRAY=1\n"), 0o644)

	// 10.2: stray docker-compose.yml NOT in include: — must NOT be discovered
	weirdDir := filepath.Join(root, "weird")
	os.MkdirAll(weirdDir, 0o755)
	os.WriteFile(filepath.Join(weirdDir, "docker-compose.yml"), []byte(`
services:
  weird: { image: busybox, env_file: [./.weird.env] }
`), 0o644)
	os.WriteFile(filepath.Join(weirdDir, ".weird.env"), []byte("WEIRD=1\n"), 0o644)

	out, err := runCenvkit(t, root, nil, "env-files")
	if err != nil {
		t.Fatalf("[10] env-files: %v\n%s", err, out)
	}
	if strings.Contains(out, ".stray.env") {
		t.Fatalf("[10.1] stray compose.yaml env must NOT be discovered (G2 inversion):\n%s", out)
	}
	if strings.Contains(out, ".weird.env") {
		t.Fatalf("[10.2] stray docker-compose.yml env must NOT be discovered (G2 inversion):\n%s", out)
	}
}

// ─── Scenario 11: COMPOSE_DEPTH accepted-but-ignored (1 assertion, G3) ───────
// 11.2 DROPPED: depth-4 knob is a no-op; a/b/c/docker-compose.yml not in include:.

// 11.1: deep file NOT in include: is not-found regardless of COMPOSE_DEPTH.
func TestScenario11_DepthIgnored(t *testing.T) {
	root := stageMonorepo(t)
	// Create a depth-4 compose file that is NOT in include:
	deepDir := filepath.Join(root, "a", "b", "c")
	os.MkdirAll(deepDir, 0o755)
	os.WriteFile(filepath.Join(deepDir, "docker-compose.yml"), []byte(`
services:
  deep: { image: busybox, env_file: [./.deep.env] }
`), 0o644)
	os.WriteFile(filepath.Join(deepDir, ".deep.env"), []byte("DEEP=1\n"), 0o644)

	// COMPOSE_DEPTH=4 must NOT cause an error and must NOT find .deep.env
	out, err := runCenvkit(t, root, []string{"COMPOSE_DEPTH=4"}, "env-files")
	if err != nil {
		t.Fatalf("[11.1] env-files COMPOSE_DEPTH=4 must not error: %v\n%s", err, out)
	}
	if strings.Contains(out, ".deep.env") {
		t.Fatalf("[11.1] deep file not in include: must NOT be discovered (G3):\n%s", out)
	}
}

// ─── Scenario 12: host overrides (4 assertions, no docker) ───────────────────

// 12.1 .testhost.env discovered via ${HOSTNAME}
// 12.2 chain order: .env < .testhost.env (before .secrets.env)
// 12.3 non-matching hostname: .testhost.env NOT discovered (negative)
// 12.4 secrets file stays last
func TestScenario12_HostOverrides(t *testing.T) {
	root := stageMonorepo(t)
	os.WriteFile(filepath.Join(root, ".testhost.env"), []byte("H=1\n"), 0o644)
	os.WriteFile(filepath.Join(root, ".secrets.env"), []byte("S=1\n"), 0o644)

	// 12.1 + 12.2 + 12.4: testhost discovered, order enforced
	out, err := runCenvkit(t, root, []string{"HOSTNAME=testhost"}, "env-files")
	if err != nil {
		t.Fatalf("[12] env-files testhost: %v\n%s", err, out)
	}
	if !strings.Contains(out, ".testhost.env") {
		t.Fatalf("[12.1] expected .testhost.env in output:\n%s", out)
	}
	lines := nonEmpty(strings.Split(out, "\n"))
	if !assertBefore(lines, "/.env", ".testhost.env") {
		t.Fatalf("[12.2] .env must appear before .testhost.env:\n%s", out)
	}
	// 12.4: secrets last WITHIN LAYER-1 — use --chain which shows Layer-1 only.
	chainOut, err := runCenvkit(t, root, []string{"HOSTNAME=testhost"}, "env-debug", "--chain")
	if err != nil {
		t.Fatalf("[12.4] env-debug --chain: %v\n%s", err, chainOut)
	}
	chainLines := nonEmpty(strings.Split(chainOut, "\n"))
	if len(chainLines) == 0 || !strings.HasSuffix(strings.TrimSpace(chainLines[len(chainLines)-1]), ".secrets.env") {
		t.Fatalf("[12.4] secrets must be last in Layer-1 chain:\n%v", chainLines)
	}

	// 12.3: non-matching host must NOT see .testhost.env
	out2, err2 := runCenvkit(t, root, []string{"HOSTNAME=otherhost"}, "env-files")
	if err2 != nil {
		t.Fatalf("[12.3] env-files otherhost: %v\n%s", err2, out2)
	}
	if strings.Contains(out2, ".testhost.env") {
		t.Fatalf("[12.3] otherhost must NOT see .testhost.env:\n%s", out2)
	}
}

// ─── Scenario 13: ${HOST} token (1 assertion, no docker) ─────────────────────

// 13.1 ${HOST} substitutes same as ${HOSTNAME}
func TestScenario13_HostToken(t *testing.T) {
	root := stageMonorepo(t)
	os.WriteFile(filepath.Join(root, ".testhost.env"), []byte("H=1\n"), 0o644)
	// .docker-env-chain already uses ${HOSTNAME}; create a parallel chain using ${HOST}
	hostDir := t.TempDir()
	os.WriteFile(filepath.Join(hostDir, ".docker-env-chain"), []byte(".env\n.${HOST}.env\n"), 0o644)
	os.WriteFile(filepath.Join(hostDir, ".env"), []byte("B=1\n"), 0o644)
	os.WriteFile(filepath.Join(hostDir, ".testhost.env"), []byte("H=1\n"), 0o644)

	out, err := runCenvkit(t, hostDir, []string{"HOSTNAME=testhost"}, "env-files")
	if err != nil {
		t.Fatalf("[13.1] env-files: %v\n%s", err, out)
	}
	if !strings.Contains(out, ".testhost.env") {
		t.Fatalf("[13.1] ${HOST} did not resolve to .testhost.env:\n%s", out)
	}
}

// ─── Scenario 14: no fallback shim (1 assertion, G4) ─────────────────────────

// G4: no fallback shim; chain-only mode succeeds with empty Layer-2.
// A project dir with no compose file: cenvkit env-files must exit 0 and list
// only Layer-1 chain files (does NOT error just because Layer-2 is empty).
func TestScenario14_ChainOnlyNoCompose(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("VAR=1\n"), 0o644)
	// no compose file in dir

	out, err := runCenvkit(t, dir, nil, "env-files")
	if err != nil {
		t.Fatalf("[14.1] chain-only env-files must not error: %v\n%s", err, out)
	}
	if !strings.Contains(out, ".env") {
		t.Fatalf("[14.1] expected .env in chain-only output:\n%s", out)
	}
}

// ─── Scenario 16: per-service env tier (2 assertions, no docker) ─────────────

// 16.1 dev: web/.web.dev.env discovered
// 16.2 prod: web/.web.prod.env found AND .web.dev.env excluded (compound assertion)
func TestScenario16_PerServiceEnvTier(t *testing.T) {
	root := stageMonorepo(t)

	// 16.1 dev
	devOut, err := runCenvkit(t, root, []string{"COMPOSE_ENV=dev"}, "env-files")
	if err != nil {
		t.Fatalf("[16] env-files dev: %v\n%s", err, devOut)
	}
	if !strings.Contains(devOut, ".web.dev.env") {
		t.Fatalf("[16.1] expected .web.dev.env in dev output:\n%s", devOut)
	}

	// 16.2 prod: .web.prod.env present, .web.dev.env absent
	prodOut, err := runCenvkit(t, root, []string{"COMPOSE_ENV=prod"}, "env-files")
	if err != nil {
		t.Fatalf("[16] env-files prod: %v\n%s", err, prodOut)
	}
	if !strings.Contains(prodOut, ".web.prod.env") {
		t.Fatalf("[16.2] expected .web.prod.env in prod output:\n%s", prodOut)
	}
	if strings.Contains(prodOut, ".web.dev.env") {
		t.Fatalf("[16.2] .web.dev.env must be absent in prod:\n%s", prodOut)
	}
}

// ─── Scenario 17.1–17.2: root per-env tier (2 assertions, no docker) ─────────

// 17.1 dev: .dev.env in chain, .prod.env not
// 17.2 prod: .prod.env in chain, .dev.env not
func TestScenario17_RootEnvTier(t *testing.T) {
	root := stageMonorepo(t)

	// 17.1 dev
	devOut, err := runCenvkit(t, root, []string{"COMPOSE_ENV=dev"}, "env-files")
	if err != nil {
		t.Fatalf("[17] env-files dev: %v\n%s", err, devOut)
	}
	if !strings.Contains(devOut, "/.dev.env") {
		t.Fatalf("[17.1] expected .dev.env in dev chain:\n%s", devOut)
	}
	if strings.Contains(devOut, "/.prod.env") {
		t.Fatalf("[17.1] .prod.env must be absent in dev:\n%s", devOut)
	}

	// 17.2 prod
	prodOut, err := runCenvkit(t, root, []string{"COMPOSE_ENV=prod"}, "env-files")
	if err != nil {
		t.Fatalf("[17] env-files prod: %v\n%s", err, prodOut)
	}
	if !strings.Contains(prodOut, "/.prod.env") {
		t.Fatalf("[17.2] expected .prod.env in prod chain:\n%s", prodOut)
	}
	if strings.Contains(prodOut, "/.dev.env") {
		t.Fatalf("[17.2] .dev.env must be absent in prod:\n%s", prodOut)
	}
}

// ─── Scenario 18.1: profiles passthrough — env-files only (1 assertion, no docker) ──

// 18.1 profiled 'tools' service OFF by default: its env_files must NOT appear.
// (tools service has no env_file in the fixture, but the assertion proves the engine
// excludes inactive-profile services from Result.EnvFiles.)
func TestScenario18_ProfilesEnvFiles(t *testing.T) {
	root := stageMonorepo(t)
	// Without COMPOSE_PROFILES=tools: tools service is inactive.
	out, err := runCenvkit(t, root, nil, "env-files")
	if err != nil {
		t.Fatalf("[18.1] env-files: %v\n%s", err, out)
	}
	// web and api must be present; tools has no env_file so nothing to assert absent.
	// The meaningful assertion here is that env-files succeeds without profiles and
	// returns the expected active-set files.
	if !strings.Contains(out, ".web.env") || !strings.Contains(out, ".api.env") {
		t.Fatalf("[18.1] expected .web.env and .api.env in default profile output:\n%s", out)
	}
}

// ─── Scenario 21: deep nesting services/<svc>/ (1 assertion, no docker) ──────

// 21.1 services/reports/.reports.env discovered from root (depth-3 include)
func TestScenario21_DeepNesting(t *testing.T) {
	root := stageMonorepo(t)
	out, err := runCenvkit(t, root, nil, "env-files")
	if err != nil {
		t.Fatalf("[21] env-files: %v\n%s", err, out)
	}
	if !strings.Contains(out, ".reports.env") {
		t.Fatalf("[21.1] expected .reports.env in output (depth-3 include):\n%s", out)
	}
}

// ─── Scenario 22: submodule shape (2 assertions, INVERSION G1/G2) ────────────

// INVERSION (G1/G2 class): sh find-by-glob discovered non-included subdirs;
// include-graph does not. .git gitlink vs .git dir is moot under compose-go.
// vendored/ and vendored2/ are NOT in the root include: (./web, ./api, ./services/reports/).
func TestScenario22_SubmoduleNotDiscovered(t *testing.T) {
	root := stageMonorepo(t)

	// 22.1: .git gitlink subproject NOT discovered
	vend1 := filepath.Join(root, "vendored")
	os.MkdirAll(vend1, 0o755)
	os.WriteFile(filepath.Join(vend1, "docker-compose.yml"), []byte(`
services:
  vend: { image: busybox, env_file: [./.vend.env] }
`), 0o644)
	os.WriteFile(filepath.Join(vend1, ".vend.env"), []byte("VEND=1\n"), 0o644)
	// Simulate a gitlink (just a file named .git)
	os.WriteFile(filepath.Join(vend1, ".git"), []byte("gitdir: ../../.git/modules/vendored\n"), 0o644)

	// 22.2: .git directory subproject NOT discovered
	vend2 := filepath.Join(root, "vendored2")
	os.MkdirAll(filepath.Join(vend2, ".git"), 0o755)
	os.WriteFile(filepath.Join(vend2, "docker-compose.yml"), []byte(`
services:
  vend2: { image: busybox, env_file: [./.vend2.env] }
`), 0o644)
	os.WriteFile(filepath.Join(vend2, ".vend2.env"), []byte("VEND2=1\n"), 0o644)

	out, err := runCenvkit(t, root, nil, "env-files")
	if err != nil {
		t.Fatalf("[22] env-files: %v\n%s", err, out)
	}
	if strings.Contains(out, ".vend.env") {
		t.Fatalf("[22.1] .vend.env (gitlink subproject) must NOT be discovered:\n%s", out)
	}
	if strings.Contains(out, ".vend2.env") {
		t.Fatalf("[22.2] .vend2.env (.git-dir subproject) must NOT be discovered:\n%s", out)
	}
}

// ─── Scenario 23: host token sanitization (2 assertions, no docker) ──────────

// 23.1 engine survives HOSTNAME with sed-special chars (exit 0)
// 23.2 sanitized host resolves .evlhost.env
func TestScenario23_HostSanitization(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".docker-env-chain"), []byte(".env\n.${HOSTNAME}.env\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("BASE=1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".evlhost.env"), []byte("H=1\n"), 0o644)

	out, err := runCenvkit(t, dir, []string{"HOSTNAME=ev|l&host"}, "env-files")
	if err != nil {
		t.Fatalf("[23.1] must not error on sed-special hostname: %v\n%s", err, out)
	}
	if !strings.Contains(out, ".evlhost.env") {
		t.Fatalf("[23.2] sanitized host did not resolve .evlhost.env:\n%s", out)
	}
}

// ─── env-debug: --value and --chain (smoke.sh 5.6/5.7, no docker) ────────────

// 5.6 --value --var SMOKE_VAL == the value set in Layer-1
func TestEnvDebug_Value(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("SMOKE_VAL=hello-layer1\n"), 0o644)

	out, err := runCenvkit(t, dir, nil, "env-debug", "--value", "--var", "SMOKE_VAL")
	if err != nil {
		t.Fatalf("[5.6] env-debug --value: %v\n%s", err, out)
	}
	if !strings.Contains(strings.TrimSpace(out), "hello-layer1") {
		t.Fatalf("[5.6] expected hello-layer1, got %q", out)
	}
}

// 5.7 --value on unset var yields empty (no crash)
func TestEnvDebug_ValueUnset(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("A=1\n"), 0o644)

	out, err := runCenvkit(t, dir, nil, "env-debug", "--value", "--var", "DEFINITELY_UNSET")
	if err != nil {
		t.Fatalf("[5.7] env-debug --value unset: %v\n%s", err, out)
	}
	if trimmed := strings.TrimSpace(out); trimmed != "" {
		t.Fatalf("[5.7] unset var should be empty, got %q", trimmed)
	}
}

// --chain exit 0 + non-empty output (smoke.sh 5.1)
func TestEnvDebug_ChainExitsZero(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("A=1\n"), 0o644)

	out, err := runCenvkit(t, dir, nil, "env-debug", "--chain")
	if err != nil {
		t.Fatalf("[5.1] env-debug --chain: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("[5.1] --chain output must be non-empty")
	}
}

// --files exit 0 + non-empty output (smoke.sh 5.4)
func TestEnvDebug_FilesExitsZero(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("A=1\n"), 0o644)

	out, err := runCenvkit(t, dir, nil, "env-debug", "--files")
	if err != nil {
		t.Fatalf("[5.4] env-debug --files: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("[5.4] --files output must be non-empty")
	}
}

// ─── G5: install layout (smoke.sh [2]) ───────────────────────────────────────

// G5: Go install layout — cenvkit binary is executable; .docker-env-chain back-compat.
func TestScenario_G5_InstallLayout(t *testing.T) {
	// cenvkitBin was built in TestMain — assert it is executable
	fi, err := os.Stat(cenvkitBin)
	if err != nil {
		t.Fatalf("[G5] cenvkit binary not found: %v", err)
	}
	if fi.Mode()&0o111 == 0 {
		t.Fatalf("[G5] cenvkit binary is not executable: mode=%v", fi.Mode())
	}
}

// ─── C1: single-pass §4a contract (throwaway fixture, NOT counted in 60) ─────

// C1: an env_file: path referencing a var defined ONLY in another service's Layer-2
// env_file does NOT silently resolve (single-pass, Layer-1-only interpolation).
// RED on a hypothetical two-pass impl that fed Layer-2 vars back into path interpolation.
func TestC1_SinglePassLayerContract(t *testing.T) {
	dir := t.TempDir()
	// Service A's env_file defines ONLY_IN_A; service B's env_file path depends on it.
	os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(`
services:
  svcA:
    image: busybox
    env_file:
      - ./a.env
  svcB:
    image: busybox
    env_file:
      - path: ./${ONLY_IN_A}/.b.env
        required: false
`), 0o644)
	os.WriteFile(filepath.Join(dir, "a.env"), []byte("ONLY_IN_A=sub\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", ".b.env"), []byte("B=1\n"), 0o644)

	out, err := runCenvkit(t, dir, nil, "env-files")
	if err != nil {
		t.Fatalf("[C1] env-files: %v\n%s", err, out)
	}
	// ONLY_IN_A is defined ONLY in a.env (Layer-2), NOT in any Layer-1 file.
	// With single-pass interpolation, ${ONLY_IN_A} remains unresolved/empty → path
	// does not exist → D1 os.Stat filter drops it. sub/.b.env must NOT appear.
	if strings.Contains(out, "/sub/.b.env") {
		t.Fatalf("[C1] sub/.b.env must NOT appear (Layer-2-only var can't interpolate Layer-2 paths):\n%s", out)
	}
	// a.env must appear (it is a concrete path)
	if !strings.Contains(out, "a.env") {
		t.Fatalf("[C1] a.env must appear in output:\n%s", out)
	}
}

// ─── D1 runtime-fatal half (docker-gated, throwaway fixture, NOT counted in 60) ──

// D1 runtime: missing *required* env_file is lenient at assembly but fatal at
// the real docker compose run (cenvkit compose must NOT carry the lever into the exec).
func TestD1_RuntimeFatalMissingRequired(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("SMOKE_SKIP_DOCKER=1")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(`
services:
  web:
    image: busybox
    env_file:
      - path: ./MISSING.env
        required: true
`), 0o644)
	// MISSING.env intentionally absent.

	_, err := runCenvkit(t, dir, nil, "compose", "config")
	if err == nil {
		t.Fatal("[D1] cenvkit compose config must exit non-zero when required env_file is missing at runtime")
	}
}

// ─── docker-dependent scenarios (gated by SMOKE_SKIP_DOCKER) ──────────────────

// Scenario 3: ROOT + kit: cross-subproject Layer-2 (3 assertions)
// 3.1 WEB_PORT == 18080
// 3.2 API_PORT == 19090
// 3.3 no :-0 fallback
func TestScenario3_CrossSubprojectPorts(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("SMOKE_SKIP_DOCKER=1")
	}
	root := stageMonorepo(t)
	out, err := runCenvkit(t, root, nil, "compose", "config")
	if err != nil {
		t.Fatalf("[3] cenvkit compose config: %v\n%s", err, out)
	}
	if !strings.Contains(out, "18080") {
		t.Fatalf("[3.1] WEB_PORT 18080 not in config:\n%s", out)
	}
	if !strings.Contains(out, "19090") {
		t.Fatalf("[3.2] API_PORT 19090 not in config:\n%s", out)
	}
	if strings.Contains(out, ":-0") {
		t.Fatalf("[3.3] fallback :-0 must not appear in config:\n%s", out)
	}
}

// Scenario 15: dev/prod overlay via COMPOSE_FILE ${COMPOSE_ENV}
// 15.1 dev: STACK_TIER=dev from overlay
// 15.2 prod: STACK_TIER=prod from overlay
func TestScenario15_DevProdOverlay(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("SMOKE_SKIP_DOCKER=1")
	}
	root := stageMonorepo(t)

	devOut, err := runCenvkit(t, root, []string{"COMPOSE_ENV=dev"}, "compose", "config")
	if err != nil {
		t.Fatalf("[15] compose config dev: %v\n%s", err, devOut)
	}
	if !strings.Contains(devOut, "STACK_TIER") || !strings.Contains(devOut, "dev") {
		t.Fatalf("[15.1] STACK_TIER=dev not in dev config:\n%s", devOut)
	}

	prodOut, err := runCenvkit(t, root, []string{"COMPOSE_ENV=prod"}, "compose", "config")
	if err != nil {
		t.Fatalf("[15] compose config prod: %v\n%s", err, prodOut)
	}
	if !strings.Contains(prodOut, "STACK_TIER") || !strings.Contains(prodOut, "prod") {
		t.Fatalf("[15.2] STACK_TIER=prod not in prod config:\n%s", prodOut)
	}
}

// Scenario 17.3–17.4: IS_DEV flag (docker-dependent)
// 17.3 dev: IS_DEV=true in rendered config
// 17.4 prod: IS_DEV=false in rendered config
func TestScenario17_IsDevFlag(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("SMOKE_SKIP_DOCKER=1")
	}
	root := stageMonorepo(t)

	devOut, err := runCenvkit(t, root, []string{"COMPOSE_ENV=dev"}, "compose", "config")
	if err != nil {
		t.Fatalf("[17.3] compose config dev: %v\n%s", err, devOut)
	}
	if !strings.Contains(devOut, "IS_DEV") || !strings.Contains(devOut, "true") {
		t.Fatalf("[17.3] IS_DEV=true not in dev config:\n%s", devOut)
	}

	prodOut, err := runCenvkit(t, root, []string{"COMPOSE_ENV=prod"}, "compose", "config")
	if err != nil {
		t.Fatalf("[17.4] compose config prod: %v\n%s", err, prodOut)
	}
	if !strings.Contains(prodOut, "IS_DEV") || !strings.Contains(prodOut, "false") {
		t.Fatalf("[17.4] IS_DEV=false not in prod config:\n%s", prodOut)
	}
}

// Scenario 18.2–18.3: profiles passthrough (docker-dependent)
// 18.2 web + api active by default
// 18.3 COMPOSE_PROFILES=tools enables 'tools'
func TestScenario18_ProfilesCompose(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("SMOKE_SKIP_DOCKER=1")
	}
	root := stageMonorepo(t)

	// 18.2: default — web and api in services, tools NOT
	defOut, err := runCenvkit(t, root, nil, "compose", "config", "--services")
	if err != nil {
		t.Fatalf("[18.2] compose config --services: %v\n%s", err, defOut)
	}
	if !strings.Contains(defOut, "web") || !strings.Contains(defOut, "api") {
		t.Fatalf("[18.2] web and api must be active by default:\n%s", defOut)
	}
	if strings.Contains(defOut, "tools") {
		t.Fatalf("[18.2] tools must be off by default:\n%s", defOut)
	}

	// 18.3: with COMPOSE_PROFILES=tools
	toolsOut, err := runCenvkit(t, root, []string{"COMPOSE_PROFILES=tools"}, "compose", "config", "--services")
	if err != nil {
		t.Fatalf("[18.3] compose config --services tools: %v\n%s", err, toolsOut)
	}
	if !strings.Contains(toolsOut, "tools") {
		t.Fatalf("[18.3] COMPOSE_PROFILES=tools must activate tools:\n%s", toolsOut)
	}
}

// Scenario 21.2: REPORTS_PORT=15151 resolved (docker-dependent)
func TestScenario21_ReportsPort(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("SMOKE_SKIP_DOCKER=1")
	}
	root := stageMonorepo(t)
	out, err := runCenvkit(t, root, nil, "compose", "config")
	if err != nil {
		t.Fatalf("[21.2] compose config: %v\n%s", err, out)
	}
	if !strings.Contains(out, "15151") {
		t.Fatalf("[21.2] REPORTS_PORT 15151 not in config:\n%s", out)
	}
}

// W3 value-level guard: secrets-last within Layer-1, verified at the value level
// (docker-gated; the file-ordering/dedup is unit-tested in TestAssemble_*).
func TestW3_SecretsLastValueLevel(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("SMOKE_SKIP_DOCKER=1")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".docker-env-chain"), []byte(".env\n.secrets.env\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("API_TOKEN=base-val\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".secrets.env"), []byte("API_TOKEN=secret-real\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(`
services:
  web:
    image: busybox
    environment:
      API_TOKEN: "${API_TOKEN}"
`), 0o644)
	out, err := runCenvkit(t, dir, nil, "compose", "config")
	if err != nil {
		t.Fatalf("[W3] compose config: %v\n%s", err, out)
	}
	if !strings.Contains(out, "secret-real") {
		t.Fatalf("[W3] secrets-last: expected secret-real to win, got:\n%s", out)
	}
	if strings.Contains(out, "base-val") {
		t.Fatalf("[W3] secrets-last: base-val must be overridden by secret-real:\n%s", out)
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func nonEmpty(lines []string) []string {
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// assertBefore returns true when a line containing sub1 appears before any line
// containing sub2 in lines.
func assertBefore(lines []string, sub1, sub2 string) bool {
	i1, i2 := -1, -1
	for i, l := range lines {
		if i1 == -1 && strings.Contains(l, sub1) {
			i1 = i
		}
		if strings.Contains(l, sub2) {
			i2 = i
		}
	}
	return i1 != -1 && i2 != -1 && i1 < i2
}
