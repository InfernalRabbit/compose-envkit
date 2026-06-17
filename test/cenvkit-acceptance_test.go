// Package acceptance ports the smoke-monorepo.sh (23 scenarios, 79 assertions after
// v3 provenance recount + --overview additions + blueprint chain-override) and
// smoke.sh suites to drive the cenvkit binary directly.
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
// v3 behavior inversions vs v1/v2 (Layer-2 becomes debug-only):
//
//	V1: run path (env-files / compose COMPOSE_ENV_FILES) is Layer-1 ONLY — service
//	    env_file: paths are NOT included (was: L1+L2). RED on a pre-v3 impl.
//	V2: ${VAR} defined only in a service env_file: falls back at run time (was: resolved).
//	V3: env-debug --trace on a service-env_file-only var reports Gap=true, runtime defs,
//	    and the fallback resolved value — not a layer2 winner.
//
// Count: exactly 78 smoke-monorepo assertions (v2 baseline 68, −1 retire 6.1,
// +5 new v3 gap/L1-only assertions, +3 prov-6 --effective inline-env invariants,
// +3 --overview mode assertions).
// C1 (§4a single-pass) and D1-runtime (§4b fatal) use throwaway fixtures, NOT counted in 78.
package acceptance

import (
	"encoding/json"
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

// v3 INVERSION (V1): run path is Layer-1 only — service env_file: paths absent.
// 4.1 web/.web.env must NOT appear in env-files output (was: present in v1/v2)
// 4.2 api/.api.env must NOT appear in env-files output (was: present in v1/v2)
// 4.3 root .env in output (Layer-1, unchanged)
func TestScenario4_EnvFilesFromRoot(t *testing.T) {
	root := stageMonorepo(t)
	out, err := runCenvkit(t, root, nil, "env-files")
	if err != nil {
		t.Fatalf("[4] env-files: %v\n%s", err, out)
	}
	// 4.1 — Layer-2 service file must NOT appear in run path (V1 inversion)
	if strings.Contains(out, ".web.env") {
		t.Fatalf("[4.1] .web.env must NOT appear in Layer-1-only env-files output:\n%s", out)
	}
	// 4.2 — Layer-2 service file must NOT appear in run path (V1 inversion)
	if strings.Contains(out, ".api.env") {
		t.Fatalf("[4.2] .api.env must NOT appear in Layer-1-only env-files output:\n%s", out)
	}
	// 4.3 — Layer-1 root chain file must still appear
	if !strings.Contains(out, "/.env") {
		t.Fatalf("[4.3] expected /.env in Layer-1 output:\n%s", out)
	}
}

// ─── Scenario 6: isolated api/ (1 assertion, −1 vs v2) ───────────────────────

// v3 RETIRE 6.1: api/ env-files no longer lists .api.env (run path is L1-only —
// 6.1 was the positive presence check; now covered by the L1-only run-path gate).
// 6.2 api/ does NOT see .web.env (sibling isolation — critical negative, unchanged)
func TestScenario6_ApiDirIsolation(t *testing.T) {
	root := stageMonorepo(t)
	apiDir := filepath.Join(root, "api")
	out, err := runCenvkit(t, apiDir, nil, "env-files")
	if err != nil {
		t.Fatalf("[6] env-files: %v\n%s", err, out)
	}
	// 6.2 — sibling isolation: api scope must never see web's env file
	if strings.Contains(out, ".web.env") {
		t.Fatalf("[6.2] api scope leaked .web.env:\n%s", out)
	}
}

// ─── Scenario 8: COMPOSE_ENV=prod (1 assertion, no docker) ───────────────────

// v3 INVERSION (V1): run path is Layer-1 only.
// 8.1 prod: service env_file: paths absent from env-files output (was: present in v1/v2)
func TestScenario8_ProdEnvFiles(t *testing.T) {
	root := stageMonorepo(t)
	out, err := runCenvkit(t, root, []string{"COMPOSE_ENV=prod"}, "env-files")
	if err != nil {
		t.Fatalf("[8] env-files prod: %v\n%s", err, out)
	}
	// 8.1 — Layer-2 service env files must NOT appear in the run path (V1 inversion)
	for _, absent := range []string{".web.env", ".api.env", ".reports.env"} {
		if strings.Contains(out, absent) {
			t.Fatalf("[8.1] %q must NOT appear in Layer-1-only prod env-files output:\n%s", absent, out)
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

// v3 REFRAME (V1): per-service env tiers are runtime-only; they no longer appear
// in env-files. Assertions move to --effective (the native per-service runtime env).
// 16.1 dev:  web service --effective includes .web.dev.env (runtime container env)
// 16.2 prod: web service --effective includes .web.prod.env and NOT .web.dev.env
func TestScenario16_PerServiceEnvTier(t *testing.T) {
	root := stageMonorepo(t)

	// 16.1 dev — per-service runtime env must include the dev tier file
	devOut, err := runCenvkit(t, root, []string{"COMPOSE_ENV=dev"}, "env-debug", "--effective", "--service", "web")
	if err != nil {
		t.Fatalf("[16] env-debug --effective --service web dev: %v\n%s", err, devOut)
	}
	if !strings.Contains(devOut, ".web.dev.env") {
		t.Fatalf("[16.1] expected .web.dev.env in dev --effective output:\n%s", devOut)
	}

	// 16.2 prod: .web.prod.env present, .web.dev.env absent
	prodOut, err := runCenvkit(t, root, []string{"COMPOSE_ENV=prod"}, "env-debug", "--effective", "--service", "web")
	if err != nil {
		t.Fatalf("[16] env-debug --effective --service web prod: %v\n%s", err, prodOut)
	}
	if !strings.Contains(prodOut, ".web.prod.env") {
		t.Fatalf("[16.2] expected .web.prod.env in prod --effective output:\n%s", prodOut)
	}
	if strings.Contains(prodOut, ".web.dev.env") {
		t.Fatalf("[16.2] .web.dev.env must be absent in prod --effective:\n%s", prodOut)
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

// v3 INVERSION (V1): run path is Layer-1 only; service env_file: paths absent.
// 18.1 env-files succeeds without profiles and returns Layer-1 files only;
//
//	service env_file: paths (.web.env, .api.env) must NOT appear.
func TestScenario18_ProfilesEnvFiles(t *testing.T) {
	root := stageMonorepo(t)
	// Without COMPOSE_PROFILES=tools: tools service is inactive.
	out, err := runCenvkit(t, root, nil, "env-files")
	if err != nil {
		t.Fatalf("[18.1] env-files: %v\n%s", err, out)
	}
	// 18.1 — v3: service env_file: paths must NOT appear in the run path (V1 inversion).
	// (tools has no env_file; web and api's files are runtime-only in v3.)
	for _, absent := range []string{".web.env", ".api.env", ".reports.env"} {
		if strings.Contains(out, absent) {
			t.Fatalf("[18.1] %q must NOT appear in Layer-1-only env-files output:\n%s", absent, out)
		}
	}
}

// ─── Scenario 21: deep nesting services/<svc>/ (1 assertion, no docker) ──────

// v3 INVERSION (V1): run path is Layer-1 only; deep service env_file: absent.
// 21.1 services/reports/.reports.env must NOT appear in env-files (run path is L1-only)
func TestScenario21_DeepNesting(t *testing.T) {
	root := stageMonorepo(t)
	out, err := runCenvkit(t, root, nil, "env-files")
	if err != nil {
		t.Fatalf("[21] env-files: %v\n%s", err, out)
	}
	// 21.1 — .reports.env is a service env_file:; must NOT appear in the run path
	if strings.Contains(out, ".reports.env") {
		t.Fatalf("[21.1] .reports.env must NOT appear in Layer-1-only env-files output (V1 inversion):\n%s", out)
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

// ─── C1: single-pass §4a contract (throwaway fixture, NOT counted in 78) ─────

// C1: an env_file: path referencing a var defined ONLY in another service's
// env_file does NOT silently resolve (single-pass, Layer-1-only interpolation).
// RED on a hypothetical two-pass impl that fed Layer-2 vars back into path interpolation.
//
// v3 update: run path is Layer-1 only — a.env is a service env_file: (Layer-2),
// so it must NOT appear in env-files output. Both assertions confirm Layer-1-only
// single-pass: sub/.b.env absent (path unresolved) AND a.env absent (Layer-2).
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
	// v3: a.env is a service env_file: (Layer-2); run path is Layer-1 only.
	// a.env must NOT appear in env-files output (was: present in v1/v2 Layer-2 list).
	if strings.Contains(out, "a.env") {
		t.Fatalf("[C1] a.env must NOT appear in Layer-1-only run path (v3: service env_file: is runtime-only):\n%s", out)
	}
}

// ─── D1 runtime-fatal half (docker-gated, throwaway fixture, NOT counted in 78) ──

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

// Scenario 3: ROOT + v3 run path (3 assertions)
// v3 INVERSION (V2): ${VAR} defined only in a service env_file: falls back at run time.
// 3.1 WEB_PORT falls back — port renders as "0:80" not "18080:80"
// 3.2 API_PORT falls back — port renders as "0:80" not "19090:80"
// 3.3 ports resolve to the 0 fallback — docker compose config expands ${WEB_PORT:-0}:80
//
//	to long-form published:"0" (the literal template string never appears in resolved output)
func TestScenario3_CrossSubprojectPorts(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("SMOKE_SKIP_DOCKER=1")
	}
	root := stageMonorepo(t)
	out, err := runCenvkit(t, root, nil, "compose", "config")
	if err != nil {
		t.Fatalf("[3] cenvkit compose config: %v\n%s", err, out)
	}
	// 3.1 — WEB_PORT must fall back (L1-only run path; 18080 is in .web.env only)
	if strings.Contains(out, "18080") {
		t.Fatalf("[3.1] WEB_PORT must fall back (not 18080) under Layer-1-only run path:\n%s", out)
	}
	// 3.2 — API_PORT must fall back (19090 is in .api.env only)
	if strings.Contains(out, "19090") {
		t.Fatalf("[3.2] API_PORT must fall back (not 19090) under Layer-1-only run path:\n%s", out)
	}
	// 3.3 — docker compose config resolves ${WEB_PORT:-0}:80 → published: "0"
	//       (the :-0 template is expanded; assert the rendered fallback value)
	if !strings.Contains(out, `published: "0"`) {
		t.Fatalf("[3.3] expected port fallback (published: \"0\") in resolved config (Layer-1-only run path):\n%s", out)
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

// Scenario 21.2: REPORTS_PORT falls back (docker-dependent)
// v3 INVERSION (V2): REPORTS_PORT defined only in .reports.env (service env_file:);
// with L1-only run path it falls back to the :-0 default.
func TestScenario21_ReportsPort(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("SMOKE_SKIP_DOCKER=1")
	}
	root := stageMonorepo(t)
	out, err := runCenvkit(t, root, nil, "compose", "config")
	if err != nil {
		t.Fatalf("[21.2] compose config: %v\n%s", err, out)
	}
	// 21.2 — REPORTS_PORT must fall back (15151 is in .reports.env only)
	if strings.Contains(out, "15151") {
		t.Fatalf("[21.2] REPORTS_PORT must fall back (not 15151) under Layer-1-only run path:\n%s", out)
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

// ─── env-debug provenance assertions (8 net-new; count 60→68→75→78) ─────────

// provenanceReport is the minimal shape we need to parse --json output for
// provenance assertions. Fields not needed by these tests are omitted.
// v3 additions: Gap/InChain/RuntimeDefs on VarTrace; Gap on Effect.
type provenanceReport struct {
	Services []struct {
		Service string `json:"service"`
		Entries []struct {
			Key    string `json:"key"`
			Value  string `json:"value"`
			Source struct {
				File  string `json:"file"`
				Layer string `json:"layer"`
			} `json:"source"`
		} `json:"entries"`
	} `json:"services"`
	Vars map[string]struct {
		Name   string `json:"name"`
		Value  string `json:"value"`
		Winner struct {
			File  string `json:"file"`
			Layer string `json:"layer"`
		} `json:"winner"`
		Effects []struct {
			Service  string `json:"service"`
			Field    string `json:"field"`
			Resolved string `json:"resolved"`
			Gap      bool   `json:"gap"` // v3: true when var is service-env_file-only
		} `json:"effects"`
		// v3 gap-detector fields
		InChain     bool `json:"in_chain"`
		RuntimeDefs []struct {
			Service string `json:"service"`
			File    string `json:"file"`
			Value   string `json:"value"`
		} `json:"runtime_defs"`
		Gap bool `json:"gap"`
	} `json:"vars"`
}

// [A + B-lite gap] --trace --var WEB_PORT human: v3 gap-detector output.
// WEB_PORT is defined only in .web.env (service env_file:, runtime-only in v3).
// v3 INVERSION (V3): was "winner is web/.web.env (layer2)"; now gap output. (2 assertions)
func TestProvenance_Trace_WEB_PORT_Human(t *testing.T) {
	root := stageMonorepo(t)
	out, err := runCenvkit(t, root, nil, "env-debug", "--trace", "--var", "WEB_PORT")
	if err != nil {
		t.Fatalf("[prov-1] env-debug --trace --var WEB_PORT: %v\n%s", err, out)
	}
	// prov-1a: gap line must be present (WEB_PORT is env_file-only → gap)
	if !strings.Contains(out, "gap") {
		t.Fatalf("[prov-1a] expected gap indicator in --trace output (WEB_PORT is service-env_file-only):\n%s", out)
	}
	// prov-1b: the runtime definition (web/.web.env) must appear in the gap evidence
	if !strings.Contains(out, "web/.web.env") {
		t.Fatalf("[prov-1b] expected web/.web.env runtime def in --trace gap output:\n%s", out)
	}
}

// [A + B-lite gap] --trace --var WEB_PORT --json: v3 gap-detector JSON fields.
// v3 INVERSION (V3): was "winner.layer==layer2"; now Gap==true, InChain==false,
// RuntimeDefs non-empty, Effect.Gap==true, Resolved is fallback value. (2 assertions)
func TestProvenance_Trace_WEB_PORT_JSON(t *testing.T) {
	root := stageMonorepo(t)
	out, err := runCenvkit(t, root, nil, "env-debug", "--trace", "--var", "WEB_PORT", "--json")
	if err != nil {
		t.Fatalf("[prov-2] env-debug --trace --var WEB_PORT --json: %v\n%s", err, out)
	}
	var rep provenanceReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("[prov-2] JSON parse failed: %v\n%s", err, out)
	}
	wp, ok := rep.Vars["WEB_PORT"]
	if !ok {
		t.Fatalf("[prov-2] WEB_PORT not in vars")
	}
	// prov-2a: Gap==true, InChain==false (WEB_PORT is service-env_file-only, not in L1 chain)
	if !wp.Gap {
		t.Fatalf("[prov-2a] WEB_PORT gap = false, want true (service-env_file-only var)")
	}
	if wp.InChain {
		t.Fatalf("[prov-2a] WEB_PORT in_chain = true, want false (not in Layer-1 chain)")
	}
	// prov-2b: RuntimeDefs non-empty with the service definition AND Effect.Gap==true
	if len(wp.RuntimeDefs) == 0 {
		t.Fatalf("[prov-2b] WEB_PORT runtime_defs empty, want at least one service def (web/.web.env)")
	}
	if len(wp.Effects) == 0 {
		t.Fatalf("[prov-2b] WEB_PORT effects empty, want at least one port/env effect")
	}
	for _, e := range wp.Effects {
		if !e.Gap {
			t.Fatalf("[prov-2b] WEB_PORT effect.gap = false for effect %+v, want true", e)
		}
	}
}

// [C] --effective --service web --json: web service has at least one entry
// whose source.layer is "env_file" or "environment". (1 assertion)
func TestProvenance_Effective_Web_JSON(t *testing.T) {
	root := stageMonorepo(t)
	out, err := runCenvkit(t, root, nil, "env-debug", "--effective", "--service", "web", "--json")
	if err != nil {
		t.Fatalf("[prov-3] env-debug --effective --service web --json: %v\n%s", err, out)
	}
	var rep provenanceReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("[prov-3] JSON parse failed: %v\n%s", err, out)
	}
	// find web service entries
	for _, svc := range rep.Services {
		if svc.Service != "web" {
			continue
		}
		for _, e := range svc.Entries {
			if e.Source.Layer == "env_file" || e.Source.Layer == "environment" {
				return // assertion satisfied
			}
		}
		t.Fatalf("[prov-3] web service entries have no env_file or environment source: %+v", svc.Entries)
	}
	t.Fatalf("[prov-3] web service not found in services: %+v", rep.Services)
}

// [C --effective inline-env interpolation] Three correctness invariants for v3:
//
//  1. Inline environment: ${X:-fallback} where X is ONLY in the service env_file:
//     → FOO shows "fallback", NOT the env_file value of X.  The C-load interpolates
//     inline environment: against the Layer-1-only env (interpEnv); service env_file:
//     values never feed that interpolation, so the :-default branch is taken.
//     RED on any impl that folds service env_file: into the interpolation mapping.
//
//  2. The env_file's own literal entry (X=envfileval) still shows with source env_file.
//     The env_file IS read for the container's runtime env — only interpolation is blocked.
//
//  3. An inline environment: value for the SAME key beats the env_file: value
//     (environment > env_file precedence in Docker Compose native semantics).
//
// (3 assertions; count: 72 + 3 = 75; +3 --overview → 78)
func TestProvenance_Effective_InlineEnvInterpolation(t *testing.T) {
	dir := t.TempDir()
	// svc.env defines X (env_file-only) and OVERRIDE_ME (beaten by inline env).
	os.WriteFile(filepath.Join(dir, "svc.env"), []byte("X=envfileval\nOVERRIDE_ME=from-envfile\n"), 0o644)
	// compose: inline environment references ${X:-fallback} and overrides OVERRIDE_ME.
	os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(`
services:
  svc:
    image: busybox
    env_file:
      - path: ./svc.env
        required: false
    environment:
      FOO: "${X:-fallback}"
      OVERRIDE_ME: "from-env-inline"
`), 0o644)

	out, err := runCenvkit(t, dir, nil, "env-debug", "--effective", "--service", "svc", "--json")
	if err != nil {
		t.Fatalf("[prov-6] env-debug --effective --service svc --json: %v\n%s", err, out)
	}
	var rep provenanceReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("[prov-6] JSON parse failed: %v\n%s", err, out)
	}
	var svcEntries []struct {
		Key    string `json:"key"`
		Value  string `json:"value"`
		Source struct {
			Layer string `json:"layer"`
		} `json:"source"`
	}
	for _, svc := range rep.Services {
		if svc.Service != "svc" {
			continue
		}
		// Re-decode into a richer type to capture the key/value/source.layer we need.
		// (We reuse the outer provenanceReport shape; svc.Entries is already decoded.)
		for _, e := range svc.Entries {
			svcEntries = append(svcEntries, struct {
				Key    string `json:"key"`
				Value  string `json:"value"`
				Source struct {
					Layer string `json:"layer"`
				} `json:"source"`
			}{Key: e.Key, Value: e.Value, Source: struct {
				Layer string `json:"layer"`
			}{Layer: e.Source.Layer}})
		}
		break
	}
	if len(svcEntries) == 0 {
		t.Fatalf("[prov-6] svc service not found or has no entries in: %+v", rep.Services)
	}
	entryMap := map[string]struct{ value, layer string }{}
	for _, e := range svcEntries {
		entryMap[e.Key] = struct{ value, layer string }{e.Value, e.Source.Layer}
	}

	// prov-6a: FOO must be "fallback" — X is env_file-only; inline env interpolates
	// against Layer-1 only, so ${X:-fallback} takes the :-fallback branch.
	if foo, ok := entryMap["FOO"]; !ok {
		t.Fatalf("[prov-6a] FOO not in --effective output")
	} else if foo.value != "fallback" {
		t.Fatalf("[prov-6a] FOO=%q, want fallback (X is env_file-only; must NOT feed inline env interpolation)", foo.value)
	}

	// prov-6b: X must appear with source.layer=env_file (runtime container env, correct).
	if x, ok := entryMap["X"]; !ok {
		t.Fatalf("[prov-6b] X not in --effective output (env_file literal must still show)")
	} else if x.layer != "env_file" {
		t.Fatalf("[prov-6b] X source.layer=%q, want env_file", x.layer)
	}

	// prov-6c: OVERRIDE_ME must be "from-env-inline" with source.layer=environment
	// (inline environment: beats env_file: for the same key).
	if om, ok := entryMap["OVERRIDE_ME"]; !ok {
		t.Fatalf("[prov-6c] OVERRIDE_ME not in --effective output")
	} else if om.value != "from-env-inline" {
		t.Fatalf("[prov-6c] OVERRIDE_ME=%q, want from-env-inline (environment: must beat env_file:)", om.value)
	} else if om.layer != "environment" {
		t.Fatalf("[prov-6c] OVERRIDE_ME source.layer=%q, want environment", om.layer)
	}
}

// [W3 provenance form] A var set in both .env (earlier) and .secrets.env (later
// in Layer-1) → --trace --var shows .secrets.env as winner (within-chain secrets-last).
// (1 assertion)
func TestProvenance_W3_SecretsLast_TraceWinner(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".docker-env-chain"), []byte(".env\n.secrets.env\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("API_TOKEN=base-val\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".secrets.env"), []byte("API_TOKEN=secret-real\n"), 0o644)

	out, err := runCenvkit(t, dir, nil, "env-debug", "--trace", "--var", "API_TOKEN")
	if err != nil {
		t.Fatalf("[prov-4] env-debug --trace --var API_TOKEN: %v\n%s", err, out)
	}
	// assertion 1: winner is .secrets.env (last-wins within Layer-1)
	if !strings.Contains(out, ".secrets.env") || !strings.Contains(out, "winner") {
		t.Fatalf("[prov-4] expected .secrets.env as winner in --trace output:\n%s", out)
	}
}

// [chain-only] A dir with no compose file → env-debug --trace --var X --json
// succeeds (exit 0), services absent/empty, var is attributed layer1. (2 assertions)
func TestProvenance_ChainOnly_JSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("CHAIN_X=val42\n"), 0o644)
	// deliberately no compose file

	out, err := runCenvkit(t, dir, nil, "env-debug", "--trace", "--var", "CHAIN_X", "--json")
	if err != nil {
		t.Fatalf("[prov-5] chain-only env-debug --json: %v\n%s", err, out)
	}
	var rep provenanceReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("[prov-5] JSON parse failed: %v\n%s", err, out)
	}
	// assertion 1: services is absent/empty (no compose file → chain-only)
	if len(rep.Services) != 0 {
		t.Fatalf("[prov-5a] chain-only: expected empty services, got %+v", rep.Services)
	}
	// assertion 2: CHAIN_X is attributed to layer1 (from the .env chain file)
	cx, ok := rep.Vars["CHAIN_X"]
	if !ok {
		t.Fatalf("[prov-5b] CHAIN_X not in vars")
	}
	if cx.Winner.Layer != "layer1" {
		t.Fatalf("[prov-5b] CHAIN_X winner.layer = %q, want layer1", cx.Winner.Layer)
	}
}

// ─── v3 new assertions (5 net-new + 3 prov-6; 68−1+5+3 = 75; +3 --overview → 78) ──

// [V1 run-path L1-only] env-files output contains NO service env_file: path. (+1, RED on pre-v3)
func TestV3_RunPath_EnvFiles_L1Only(t *testing.T) {
	root := stageMonorepo(t)
	out, err := runCenvkit(t, root, nil, "env-files")
	if err != nil {
		t.Fatalf("[v3-run-1] env-files: %v\n%s", err, out)
	}
	// All three known service env_file: paths must be absent from the run list.
	for _, absent := range []string{".web.env", ".api.env", ".reports.env"} {
		if strings.Contains(out, absent) {
			t.Fatalf("[v3-run-1] service env_file path %q must NOT appear in Layer-1-only env-files:\n%s", absent, out)
		}
	}
}

// [V1 run-path L1-only via compose] COMPOSE_ENV_FILES set by `cenvkit compose` carries
// NO service env_file: path. (+1, RED on pre-v3; docker-gated for compose exec)
func TestV3_RunPath_Compose_L1Only(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("SMOKE_SKIP_DOCKER=1")
	}
	root := stageMonorepo(t)
	// Use `cenvkit compose config --services` to verify that env is assembled
	// without service env_file: paths. The config succeeds and the known service
	// env_file: vars (WEB_PORT, API_PORT) are NOT resolved — they fall back.
	out, err := runCenvkit(t, root, nil, "compose", "config")
	if err != nil {
		t.Fatalf("[v3-run-2] cenvkit compose config: %v\n%s", err, out)
	}
	// service-env_file-only vars fall back to their :-default (0), not their defined values
	if strings.Contains(out, "18080") || strings.Contains(out, "19090") || strings.Contains(out, "15151") {
		t.Fatalf("[v3-run-2] service-env_file-only port values must NOT appear in config (L1-only run path):\n%s", out)
	}
}

// [gap --json: runtime_defs / in_chain fields present] gap detector JSON output carries
// the v3 gap fields. (+1, RED on pre-v3 impl without gap fields)
func TestV3_Gap_JSON_Fields(t *testing.T) {
	root := stageMonorepo(t)
	out, err := runCenvkit(t, root, nil, "env-debug", "--trace", "--var", "API_PORT", "--json")
	if err != nil {
		t.Fatalf("[v3-gap-1] env-debug --trace --var API_PORT --json: %v\n%s", err, out)
	}
	var rep provenanceReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("[v3-gap-1] JSON parse failed: %v\n%s", err, out)
	}
	ap, ok := rep.Vars["API_PORT"]
	if !ok {
		t.Fatalf("[v3-gap-1] API_PORT not in vars")
	}
	// API_PORT is defined only in .api.env (service env_file:) → gap
	if !ap.Gap {
		t.Fatalf("[v3-gap-1] API_PORT gap = false, want true (service-env_file-only var)")
	}
	if ap.InChain {
		t.Fatalf("[v3-gap-1] API_PORT in_chain = true, want false")
	}
	if len(ap.RuntimeDefs) == 0 {
		t.Fatalf("[v3-gap-1] API_PORT runtime_defs empty, want api/.api.env def")
	}
}

// [no-false-gap: chain var] A var present in the Layer-1 chain reports Gap=false
// and a normal winner. (+1, RED on an over-eager gap impl)
func TestV3_NoFalseGap_ChainVar(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("CHAIN_VAR=layer1-val\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(`
services:
  web:
    image: busybox
    environment:
      CHAIN_VAR: "${CHAIN_VAR:-default}"
`), 0o644)

	out, err := runCenvkit(t, dir, nil, "env-debug", "--trace", "--var", "CHAIN_VAR", "--json")
	if err != nil {
		t.Fatalf("[v3-nogap-1] env-debug --trace --var CHAIN_VAR --json: %v\n%s", err, out)
	}
	var rep provenanceReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("[v3-nogap-1] JSON parse failed: %v\n%s", err, out)
	}
	cv, ok := rep.Vars["CHAIN_VAR"]
	if !ok {
		t.Fatalf("[v3-nogap-1] CHAIN_VAR not in vars")
	}
	// CHAIN_VAR is in Layer-1 → must NOT be flagged as a gap
	if cv.Gap {
		t.Fatalf("[v3-nogap-1] CHAIN_VAR gap = true, want false (var IS in the Layer-1 chain)")
	}
	if !cv.InChain {
		t.Fatalf("[v3-nogap-1] CHAIN_VAR in_chain = false, want true")
	}
	if cv.Winner.Layer != "layer1" {
		t.Fatalf("[v3-nogap-1] CHAIN_VAR winner.layer = %q, want layer1", cv.Winner.Layer)
	}
}

// ─── env-debug --overview acceptance assertions (+3 → count 78) ──────────────

// [overview-1] chain section: two-file chain with a + (new) and ~ (override) marker.
// Uses a scratch fixture (not examples/monorepo) so we control the exact keys.
// Daemon-free: no compose file needed for the chain-only overview path.
func TestOverview_ChainMarkers(t *testing.T) {
	dir := t.TempDir()
	// .docker-env-chain with two files
	os.WriteFile(filepath.Join(dir, ".docker-env-chain"), []byte(".env\n.dev.env\n"), 0o644)
	// .env: defines SITE_URL (will be overridden) and BASE_KEY (new, not overridden)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("SITE_URL=example.com\nBASE_KEY=base\n"), 0o644)
	// .dev.env: overrides SITE_URL (~) and adds IS_DEV (+)
	os.WriteFile(filepath.Join(dir, ".dev.env"), []byte("SITE_URL=dev.example.com\nIS_DEV=true\n"), 0o644)

	out, err := runCenvkit(t, dir, nil, "env-debug", "--overview")
	if err != nil {
		t.Fatalf("[overview-1] env-debug --overview: %v\n%s", err, out)
	}
	// overview-1a: chain section header must be present
	if !strings.Contains(out, "Interpolation chain") {
		t.Fatalf("[overview-1a] expected 'Interpolation chain' section header:\n%s", out)
	}
	// overview-1b: .dev.env must appear in the chain section
	if !strings.Contains(out, ".dev.env") {
		t.Fatalf("[overview-1b] expected .dev.env in chain section:\n%s", out)
	}
	// overview-1c: SITE_URL must show an override (~) marker — defined in .env,
	// then overridden in .dev.env. This is the key chain-marker assertion.
	if !strings.Contains(out, "~ SITE_URL") {
		t.Fatalf("[overview-1c] expected '~ SITE_URL' (override marker) in chain section:\n%s", out)
	}
	// overview-1d: IS_DEV first defined in .dev.env → + marker
	if !strings.Contains(out, "+ IS_DEV") {
		t.Fatalf("[overview-1d] expected '+ IS_DEV' (new key marker) in chain section:\n%s", out)
	}
}

// [overview-2] runtime section: web service shows its .web.env layer.
// Daemon-free. web/.web.env is git-tracked and already staged by stageMonorepo's
// cp -R — no seeding needed (SF-2: root dotfiles are gitignored/seeded via init,
// SERVICE dotfiles like web/.web.env are git-tracked and present after cp -R).
func TestOverview_RuntimeWebLayer(t *testing.T) {
	root := stageMonorepo(t)
	out, err := runCenvkit(t, root, []string{"COMPOSE_ENV=dev"}, "env-debug", "--overview")
	if err != nil {
		t.Fatalf("[overview-2] env-debug --overview: %v\n%s", err, out)
	}
	// overview-2a: runtime-only section header must be present
	if !strings.Contains(out, "Runtime-only") {
		t.Fatalf("[overview-2a] expected 'Runtime-only' section header:\n%s", out)
	}
	// overview-2b: the web: service heading line must appear (N-3: "web" alone is
	// too weak — it matches the path, the title, etc.; assert the heading form).
	if !strings.Contains(out, "web:") {
		t.Fatalf("[overview-2b] expected 'web:' service heading in runtime section:\n%s", out)
	}
	// overview-2c: web service's .web.env layer must appear
	if !strings.Contains(out, ".web.env") {
		t.Fatalf("[overview-2c] expected .web.env layer in web service block:\n%s", out)
	}
}

// [overview-3] WEB_PORT gap annotation: the gap line must appear for WEB_PORT
// (defined only in web/.web.env, not in the Layer-1 chain). Daemon-free.
// web/.web.env is git-tracked and already staged by stageMonorepo's cp -R
// (SF-2: no seeding needed — service dotfiles are tracked, unlike root dotfiles).
func TestOverview_WEBPORTGap(t *testing.T) {
	root := stageMonorepo(t)
	out, err := runCenvkit(t, root, []string{"COMPOSE_ENV=dev"}, "env-debug", "--overview")
	if err != nil {
		t.Fatalf("[overview-3] env-debug --overview: %v\n%s", err, out)
	}
	// overview-3: gap annotation for WEB_PORT must appear
	// WEB_PORT is in web/.web.env (runtime-only) and is referenced as ${WEB_PORT:-0}
	// in web/docker-compose.yml → the gap detector flags it → --overview renders ⚠ gap.
	if !strings.Contains(out, "gap") {
		t.Fatalf("[overview-3] expected gap annotation for WEB_PORT in --overview output:\n%s", out)
	}
	if !strings.Contains(out, "WEB_PORT") {
		t.Fatalf("[overview-3] expected WEB_PORT in gap annotation:\n%s", out)
	}
}

// [overview-4] chain override on the real blueprint: example.dev.env overrides
// SITE_URL (defined in example.env), so the staged monorepo shows ~ for it.
// Guards the blueprint fixture teaching purpose: the ~ marker must appear on
// actual monorepo files, not just the scratch-fixture overview-1 test.
func TestOverview_ChainOverrideOnMonorepo(t *testing.T) {
	root := stageMonorepo(t)
	out, err := runCenvkit(t, root, []string{"COMPOSE_ENV=dev"}, "env-debug", "--overview")
	if err != nil {
		t.Fatalf("[overview-4] env-debug --overview: %v\n%s", err, out)
	}
	// overview-4: SITE_URL is defined in example.env (base) and overridden in
	// example.dev.env (.dev.env when seeded) — must render as ~ SITE_URL.
	if !strings.Contains(out, "~ SITE_URL") {
		t.Fatalf("[overview-4] expected '~ SITE_URL' (chain override marker) in blueprint --overview:\n%s", out)
	}
}

// ─── end --overview acceptance assertions ─────────────────────────────────────

// [no-false-gap: unset everywhere] A var unset in both L1 chain and all service
// env_file:s is NOT flagged as a gap. (+1, RED on an over-eager gap impl)
func TestV3_NoFalseGap_UnsetEverywhere(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("OTHER=val\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(`
services:
  web:
    image: busybox
    environment:
      TOTALLY_UNSET: "${TOTALLY_UNSET:-fallback}"
`), 0o644)

	out, err := runCenvkit(t, dir, nil, "env-debug", "--trace", "--var", "TOTALLY_UNSET", "--json")
	if err != nil {
		t.Fatalf("[v3-nogap-2] env-debug --trace --var TOTALLY_UNSET --json: %v\n%s", err, out)
	}
	var rep provenanceReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("[v3-nogap-2] JSON parse failed: %v\n%s", err, out)
	}
	uv, ok := rep.Vars["TOTALLY_UNSET"]
	if !ok {
		// var might be absent from the map if not referenced at all — that is fine (not a gap)
		return
	}
	// If present: must NOT be flagged as a gap (unset everywhere → runtime_defs empty)
	if uv.Gap {
		t.Fatalf("[v3-nogap-2] TOTALLY_UNSET gap = true, want false (unset everywhere)")
	}
	if len(uv.RuntimeDefs) != 0 {
		t.Fatalf("[v3-nogap-2] TOTALLY_UNSET runtime_defs non-empty, want empty: %+v", uv.RuntimeDefs)
	}
}

// ─── color / ANSI no-leak guards (NOT counted in 78) ────────────────────────
//
// The 78 acceptance assertions above run non-TTY/CI, so their literal-string
// matches already guard against leaked ANSI escapes (a stray \x1b would break
// strings.Contains checks). These extra tests make the no-leak contract
// explicit and also verify --color=always forces ANSI when requested.

// TestColor_NoLeak_DefaultNonTTY: the default (auto) color mode on a non-TTY
// process produces no ANSI escapes. This is also an implicit guard for all 78
// acceptance assertions above — they all run in non-TTY environments.
func TestColor_NoLeak_DefaultNonTTY(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("SITE_URL=example.com\n"), 0o644)

	// Run WITHOUT --color flag (auto) and WITHOUT NO_COLOR/CLICOLOR_FORCE set.
	// In a non-TTY test runner, auto resolves to plain.
	out, err := runCenvkit(t, dir, nil, "env-debug", "--chain")
	if err != nil {
		t.Fatalf("[color-noleak-1] env-debug --chain: %v\n%s", err, out)
	}
	if strings.Contains(out, "\x1b") {
		t.Fatalf("[color-noleak-1] auto mode (non-TTY) leaked ANSI into --chain output:\n%s", out)
	}
}

// TestColor_NoLeak_JSON: --json path must produce no ANSI escapes regardless
// of --color=always (JSON is machine output; top rule in §5).
func TestColor_NoLeak_JSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("SITE_URL=example.com\n"), 0o644)

	out, err := runCenvkit(t, dir, nil, "env-debug", "--chain", "--json", "--color=always")
	if err != nil {
		t.Fatalf("[color-noleak-2] env-debug --chain --json --color=always: %v\n%s", err, out)
	}
	if strings.Contains(out, "\x1b") {
		t.Fatalf("[color-noleak-2] --json with --color=always leaked ANSI — JSON must always be plain:\n%s", out)
	}
}

// TestColor_NoLeak_NeverFlag: --color=never produces no ANSI even when a TTY
// might be present.
func TestColor_NoLeak_NeverFlag(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("SITE_URL=example.com\n"), 0o644)

	out, err := runCenvkit(t, dir, nil, "env-debug", "--chain", "--color=never")
	if err != nil {
		t.Fatalf("[color-noleak-3] env-debug --chain --color=never: %v\n%s", err, out)
	}
	if strings.Contains(out, "\x1b") {
		t.Fatalf("[color-noleak-3] --color=never leaked ANSI:\n%s", out)
	}
}

// TestColor_NoLeak_NOCOLOR: NO_COLOR=1 env var disables ANSI output.
func TestColor_NoLeak_NOCOLOR(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("SITE_URL=example.com\n"), 0o644)

	out, err := runCenvkit(t, dir, []string{"NO_COLOR=1"}, "env-debug", "--chain")
	if err != nil {
		t.Fatalf("[color-noleak-4] env-debug --chain NO_COLOR=1: %v\n%s", err, out)
	}
	if strings.Contains(out, "\x1b") {
		t.Fatalf("[color-noleak-4] NO_COLOR=1 leaked ANSI:\n%s", out)
	}
}

// TestColor_AlwaysFlag_OverviewHasANSI: --color=always forces ANSI escapes
// in --overview output even in a non-TTY environment. This is the positive
// case that confirms --color=always is wired end-to-end through the binary.
// Uses a scratch fixture (not stageMonorepo) for speed.
func TestColor_AlwaysFlag_OverviewHasANSI(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".docker-env-chain"), []byte(".env\n.dev.env\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("SITE_URL=example.com\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".dev.env"), []byte("IS_DEV=true\n"), 0o644)

	out, err := runCenvkit(t, dir, nil, "env-debug", "--overview", "--color=always")
	if err != nil {
		t.Fatalf("[color-always-1] env-debug --overview --color=always: %v\n%s", err, out)
	}
	// --color=always must force ANSI even in non-TTY
	if !strings.Contains(out, "\x1b") {
		t.Fatalf("[color-always-1] --color=always produced no ANSI in --overview output:\n%s", out)
	}
}

// ─── end color / ANSI no-leak guards ─────────────────────────────────────────

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
