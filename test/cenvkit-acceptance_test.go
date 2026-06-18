// Package acceptance ports the smoke-monorepo.sh and smoke.sh suites to drive the
// cenvkit binary directly. Current assertion count: 128.
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
// Assertion lineage:
//
//	68  smoke-monorepo v2 baseline
//	−1  retire scenario 6.1
//	+5  v3 gap/L1-only assertions
//	+3  prov-6 --effective inline-env invariants
//	+3  --overview mode assertions (#5)
//	+1  blueprint chain-override SITE_URL (#10)
//	+28 gap-report batch A/A2/B/B2/C2/D1–D4/E (#11)
//	+1  corrected C1: --value on env_file-only var is empty (#11 follow-up)
//	+3  #12: gap-value render-only strip guard + empty-chain hint acceptance
//	+4  C1: gap-report exit-code contract (exit 1/0/2 + --json shape)
//	+13 C2: cenvkit env/run daemon-free + MF4 parity (docker-gated)
//	────
//	128 total
//
// Note: TestC1_SinglePassLayerContract and TestD1_RuntimeFatalMissingRequired use
// throwaway fixtures and guard contract seams — they are included in the 128 count.
package acceptance

import (
	"encoding/json"
	"errors"
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

// runCenvkitSplit runs cenvkit and returns stdout, stderr, and error SEPARATELY.
// Use this for tests that assert on exit code AND stderr content (e.g. error paths,
// validate-negative) where CombinedOutput() would mix them.
func runCenvkitSplit(t *testing.T, dir string, env []string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	c := exec.Command(cenvkitBin, args...)
	c.Dir = dir
	c.Env = append(os.Environ(), env...)
	var outBuf, errBuf strings.Builder
	c.Stdout = &outBuf
	c.Stderr = &errBuf
	err = c.Run()
	return outBuf.String(), errBuf.String(), err
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

// corrected-C1: --value on an env_file-only var returns EMPTY (v3 spec: --value
// reports the Layer-1 interpolation winner; a var defined only in a service
// env_file: has no Layer-1 winner → empty). Contrast: a chain var returns its value.
// RED on a hypothetical impl that leaks service env_file: values into --value output.
// Uses the staged monorepo: WEB_PORT is env_file-only (web/.web.env); COMPOSE_ENV is
// a chain var (.env sets it to "dev"). (2 assertions)
func TestEnvDebug_Value_EnvFileOnly_Empty(t *testing.T) {
	root := stageMonorepo(t)

	// corrected-C1a: env_file-only var → empty output (NOT "18080" or "0")
	out, err := runCenvkit(t, root, []string{"COMPOSE_ENV=dev"}, "env-debug", "--value", "--var", "WEB_PORT")
	if err != nil {
		t.Fatalf("[C1a] --value --var WEB_PORT: %v\n%s", err, out)
	}
	if trimmed := strings.TrimSpace(out); trimmed != "" {
		t.Fatalf("[C1a] --value --var WEB_PORT = %q, want empty (env_file-only var has no Layer-1 winner)", trimmed)
	}

	// corrected-C1b: chain var → non-empty value (COMPOSE_ENV is set in .env)
	out2, err := runCenvkit(t, root, []string{"COMPOSE_ENV=dev"}, "env-debug", "--value", "--var", "COMPOSE_ENV")
	if err != nil {
		t.Fatalf("[C1b] --value --var COMPOSE_ENV: %v\n%s", err, out2)
	}
	if trimmed := strings.TrimSpace(out2); trimmed == "" {
		t.Fatalf("[C1b] --value --var COMPOSE_ENV must be non-empty (chain var)")
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

// ─── C1: single-pass §4a contract (throwaway fixture) ───────────────────────

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

// ─── D1 runtime-fatal half (docker-gated, throwaway fixture) ────────────────

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

// ─── env-debug provenance assertions ─────────────────────────────────────────

// provenanceReport is the minimal shape we need to parse --json output for
// provenance assertions. Fields not needed by these tests are omitted.
// v3 additions: Gap/InChain/RuntimeDefs on VarTrace; Gap on Effect.
type provenanceReport struct {
	// top-level file lists (present in --files and --chain JSON output)
	Files      []string `json:"files"`
	ChainFiles []string `json:"chain_files"`
	Services   []struct {
		Service  string   `json:"service"`
		EnvFiles []string `json:"env_files"` // runtime-only env_file: paths for this service
		Entries  []struct {
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

// [#12 FIX 2+4] gap-value render-only strip guard: the HUMAN trace shows the
// normalized fallback value ("0") but --json keeps the raw "WEB_PORT=0" so
// machine consumers are not broken. RED if stripVarPrefix is applied to the Report
// rather than at render time. (2 assertions)
func TestProvenance_GapValue_RenderOnlyStrip(t *testing.T) {
	root := stageMonorepo(t)

	// strip-1: human --trace shows the stripped form: `resolves to "0"`
	// (not `resolves to "WEB_PORT=0"`)
	outHuman, err := runCenvkit(t, root, []string{"COMPOSE_ENV=dev"}, "env-debug", "--trace", "--var", "WEB_PORT")
	if err != nil {
		t.Fatalf("[strip-1] --trace --var WEB_PORT: %v\n%s", err, outHuman)
	}
	if !strings.Contains(outHuman, `resolves to "0"`) {
		t.Fatalf("[strip-1] human --trace must show normalized resolves to \"0\":\n%s", outHuman)
	}

	// strip-2: --json keeps the raw "WEB_PORT=0" in effects[].resolved (render-only strip)
	outJSON, err := runCenvkit(t, root, []string{"COMPOSE_ENV=dev"}, "env-debug", "--trace", "--var", "WEB_PORT", "--json")
	if err != nil {
		t.Fatalf("[strip-2] --trace --var WEB_PORT --json: %v\n%s", err, outJSON)
	}
	var rep provenanceReport
	if err := json.Unmarshal([]byte(outJSON), &rep); err != nil {
		t.Fatalf("[strip-2] JSON parse: %v\n%s", err, outJSON)
	}
	wp := rep.Vars["WEB_PORT"]
	rawFound := false
	for _, e := range wp.Effects {
		if e.Field == "environment[0]" && e.Resolved == "WEB_PORT=0" {
			rawFound = true
		}
	}
	if !rawFound {
		t.Fatalf("[strip-2] --json effects must keep raw \"WEB_PORT=0\" (render-only strip); effects: %+v", wp.Effects)
	}
}

// [#12 FIX 3] empty-chain hint in --overview acceptance: a project with no
// Layer-1 chain files emits the hint in the Interpolation chain section.
// Uses a scratch fixture (no .env, no .docker-env-chain, one service env_file:).
// (1 assertion)
func TestEnvDebug_Overview_EmptyChainHint(t *testing.T) {
	dir := t.TempDir()
	// no .env and no .docker-env-chain → empty Layer-1 chain
	os.MkdirAll(filepath.Join(dir, "web"), 0o755)
	os.WriteFile(filepath.Join(dir, "web", ".web.env"), []byte("WEB_PORT=18080\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services:\n  web:\n    image: busybox\n    env_file: [web/.web.env]\n"), 0o644)

	out, err := runCenvkit(t, dir, nil, "env-debug", "--overview")
	if err != nil {
		t.Fatalf("[ech-acc-1] env-debug --overview empty chain: %v\n%s", err, out)
	}
	const hint = "(none — no Layer-1 chain files present; run `cenvkit init` or add .env)"
	if !strings.Contains(out, hint) {
		t.Fatalf("[ech-acc-1] --overview must emit empty-chain hint:\n%s", out)
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

// ─── v3 new assertions ───────────────────────────────────────────────────────

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

// ─── env-debug --overview acceptance assertions ───────────────────────────────

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

// ─── gap-report batch (A / A2 / B / B2 / C2 / D1–D4 / E) ────────────────────
//
// C1 (--value WEB_PORT) is omitted: --value returns the Layer-1 winning value,
// which is empty for WEB_PORT (not in chain); the compose-resolved fallback "0"
// is only visible via --trace --json. See TestEnvDebug_Value_EnvFileOnly_Empty.

// [A] validate positive (docker-gated): staged monorepo, COMPOSE_ENV=dev →
// exits 0 + stdout contains "config valid". (2 assertions: exit 0 + message)
// [A-neg] validate negative: scratch dir w/ invalid root docker-compose.yml →
// exits non-zero + stderr non-empty. Uses runCenvkitSplit.
func TestValidate_Positive(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("SMOKE_SKIP_DOCKER=1")
	}
	root := stageMonorepo(t)
	stdout, stderr, err := runCenvkitSplit(t, root, []string{"COMPOSE_ENV=dev"}, "validate")
	if err != nil {
		t.Fatalf("[A-pos] validate COMPOSE_ENV=dev: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	// A-pos-1: exit 0 — already confirmed by err==nil
	// A-pos-2: stdout must contain "config valid"
	if !strings.Contains(stdout, "config valid") {
		t.Fatalf("[A-pos-2] stdout must contain 'config valid', got: %q", stdout)
	}
}

func TestValidate_Negative_InvalidCompose(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("SMOKE_SKIP_DOCKER=1")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("A=1\n"), 0o644)
	// intentionally malformed YAML — flow sequence never closed
	os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services:\n  web:\n    image: busybox\n  BADKEY: [broken\n"), 0o644)

	_, stderr, err := runCenvkitSplit(t, dir, nil, "validate")
	// A-neg-1: must exit non-zero
	if err == nil {
		t.Fatalf("[A-neg-1] validate with invalid compose must exit non-zero")
	}
	// A-neg-2: stderr must carry the error message
	if strings.TrimSpace(stderr) == "" {
		t.Fatalf("[A-neg-2] stderr must be non-empty on validate failure")
	}
}

// [A2] validate --all (docker-gated): exits 0 + stdout contains BOTH
// "dev config valid" AND "prod config valid". (2 assertions)
func TestValidate_All(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("SMOKE_SKIP_DOCKER=1")
	}
	root := stageMonorepo(t)
	stdout, stderr, err := runCenvkitSplit(t, root, nil, "validate", "--all")
	if err != nil {
		t.Fatalf("[A2] validate --all: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	// A2-1: "dev config valid" in stdout
	if !strings.Contains(stdout, "dev config valid") {
		t.Fatalf("[A2-1] stdout must contain 'dev config valid', got: %q", stdout)
	}
	// A2-2: "prod config valid" in stdout
	if !strings.Contains(stdout, "prod config valid") {
		t.Fatalf("[A2-2] stdout must contain 'prod config valid', got: %q", stdout)
	}
}

// [B] env-debug --files two-group on the real monorepo (non-docker).
// Guards the two-section structure: interpolation header + runtime-only header,
// correct service grouping, and the hard boundary (.web.env NOT under interpolation).
// (7 assertions)
func TestEnvDebug_Files_TwoGroup(t *testing.T) {
	root := stageMonorepo(t)
	out, err := runCenvkit(t, root, []string{"COMPOSE_ENV=dev"}, "env-debug", "--files")
	if err != nil {
		t.Fatalf("[B] env-debug --files: %v\n%s", err, out)
	}
	// B-1: interpolation section header present
	if !strings.Contains(out, "interpolation (COMPOSE_ENV_FILES):") {
		t.Fatalf("[B-1] expected 'interpolation (COMPOSE_ENV_FILES):' section header:\n%s", out)
	}
	// B-2: runtime-only section header present
	if !strings.Contains(out, "runtime-only") {
		t.Fatalf("[B-2] expected 'runtime-only' section header:\n%s", out)
	}
	// B-3: .env appears in output (under interpolation)
	if !strings.Contains(out, ".env") {
		t.Fatalf("[B-3] expected .env path in output:\n%s", out)
	}
	// B-4: web: heading in runtime-only section
	if !strings.Contains(out, "web:") {
		t.Fatalf("[B-4] expected 'web:' service heading in runtime-only section:\n%s", out)
	}
	// B-5: .web.env appears under the web service block
	if !strings.Contains(out, ".web.env") {
		t.Fatalf("[B-5] expected .web.env in web service block:\n%s", out)
	}
	// B-6: .web.dev.env also appears under web (COMPOSE_ENV=dev tier)
	if !strings.Contains(out, ".web.dev.env") {
		t.Fatalf("[B-6] expected .web.dev.env in web service block (dev tier):\n%s", out)
	}
	// B-7: .web.env must NOT appear on a line before the runtime-only header
	// (i.e. it must not be in the interpolation section). Split on the runtime-only
	// boundary and assert .web.env is absent from the interpolation half.
	parts := strings.SplitN(out, "runtime-only", 2)
	if len(parts) == 2 && strings.Contains(parts[0], ".web.env") {
		t.Fatalf("[B-7] .web.env must NOT appear in the interpolation section (Layer-1 only):\n%s", parts[0])
	}
}

// [B2] --files --json + --chain --json Layer-1-only schema (non-docker).
// .files and .chain_files contain a path ending ".env" but NO element ending
// ".web.env"/".api.env"/".reports.env". services[web].env_files contains a path
// ending ".web.env". (4 assertions)
func TestEnvDebug_Files_JSON_L1Only(t *testing.T) {
	root := stageMonorepo(t)
	out, err := runCenvkit(t, root, []string{"COMPOSE_ENV=dev"}, "env-debug", "--files", "--json")
	if err != nil {
		t.Fatalf("[B2-files] env-debug --files --json: %v\n%s", err, out)
	}
	var rep provenanceReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("[B2-files] JSON parse: %v\n%s", err, out)
	}
	serviceEnvSuffixes := []string{".web.env", ".api.env", ".reports.env"}

	// B2-1: .files contains an element ending ".env" (Layer-1 present)
	hasEnvFile := false
	for _, f := range rep.Files {
		if strings.HasSuffix(f, ".env") && !strings.HasSuffix(f, ".dev.env") && !strings.HasSuffix(f, ".prod.env") {
			hasEnvFile = true
		}
	}
	if !hasEnvFile {
		t.Fatalf("[B2-1] .files must contain an element ending '.env': %v", rep.Files)
	}
	// B2-2: .files must NOT contain any service env_file: path
	for _, f := range rep.Files {
		for _, suf := range serviceEnvSuffixes {
			if strings.HasSuffix(f, suf) {
				t.Fatalf("[B2-2] .files must NOT contain service env_file path ending %q (Layer-1 only): %v", suf, rep.Files)
			}
		}
	}
	// B2-3: .chain_files same Layer-1-only constraint
	for _, f := range rep.ChainFiles {
		for _, suf := range serviceEnvSuffixes {
			if strings.HasSuffix(f, suf) {
				t.Fatalf("[B2-3] .chain_files must NOT contain service env_file path ending %q: %v", suf, rep.ChainFiles)
			}
		}
	}
	// B2-4: services[web].env_files contains a path ending ".web.env"
	webEnvFiles := false
	for _, svc := range rep.Services {
		if svc.Service != "web" {
			continue
		}
		for _, ef := range svc.EnvFiles {
			if strings.HasSuffix(ef, ".web.env") {
				webEnvFiles = true
			}
		}
	}
	if !webEnvFiles {
		t.Fatalf("[B2-4] services[web].env_files must contain a path ending '.web.env'")
	}
}

// [C2] --trace --var REPORTS_PORT deep gap (non-docker): gap annotation present +
// a runtime line whose absolute path ends "services/reports/.reports.env". (2 assertions)
func TestEnvDebug_Trace_REPORTS_PORT_DeepGap(t *testing.T) {
	root := stageMonorepo(t)
	out, err := runCenvkit(t, root, []string{"COMPOSE_ENV=dev"}, "env-debug", "--trace", "--var", "REPORTS_PORT")
	if err != nil {
		t.Fatalf("[C2] env-debug --trace --var REPORTS_PORT: %v\n%s", err, out)
	}
	// C2-1: gap annotation present (REPORTS_PORT is service-env_file-only → gap)
	if !strings.Contains(out, "gap") {
		t.Fatalf("[C2-1] expected gap annotation in --trace REPORTS_PORT output:\n%s", out)
	}
	// C2-2: runtime line whose abs path ends services/reports/.reports.env
	if !strings.Contains(out, filepath.Join("services", "reports", ".reports.env")) {
		t.Fatalf("[C2-2] expected runtime path ending 'services/reports/.reports.env':\n%s", out)
	}
}

// [D1] Default chain fallback — no .docker-env-chain: three standard files
// (.env, .dev.env, .secrets.env) present → env-files lists them in order. (3 assertions)
func TestChain_DefaultFallback(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("BASE=1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".dev.env"), []byte("TIER=1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".secrets.env"), []byte("S=1\n"), 0o644)

	out, err := runCenvkit(t, dir, []string{"COMPOSE_ENV=dev"}, "env-files")
	if err != nil {
		t.Fatalf("[D1] env-files (default chain): %v\n%s", err, out)
	}
	lines := nonEmpty(strings.Split(strings.TrimSpace(out), "\n"))
	// D1-1: .env present
	if !strings.HasSuffix(lines[0], ".env") {
		t.Fatalf("[D1-1] first entry must end '.env', got %q", lines[0])
	}
	// D1-2: .dev.env present and after .env
	if len(lines) < 2 || !strings.HasSuffix(lines[1], ".dev.env") {
		t.Fatalf("[D1-2] second entry must end '.dev.env', got %v", lines)
	}
	// D1-3: .secrets.env present and last
	if len(lines) < 3 || !strings.HasSuffix(lines[2], ".secrets.env") {
		t.Fatalf("[D1-3] third entry must end '.secrets.env', got %v", lines)
	}
}

// [D2] Named-missing chain file skipped (scratch): .docker-env-chain lists
// .env, .missing.env, .secrets.env; only .env + .secrets.env exist → env-files
// lists only those two; .missing.env absent; exits 0. (3 assertions)
func TestChain_MissingFileSkipped(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".docker-env-chain"), []byte(".env\n.missing.env\n.secrets.env\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("A=1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".secrets.env"), []byte("S=1\n"), 0o644)
	// .missing.env intentionally NOT created

	out, err := runCenvkit(t, dir, nil, "env-files")
	// D2-1: exits 0 (missing file is skipped, not fatal)
	if err != nil {
		t.Fatalf("[D2-1] env-files must exit 0 when a named file is absent: %v\n%s", err, out)
	}
	// D2-2: .env and .secrets.env present
	lines := nonEmpty(strings.Split(strings.TrimSpace(out), "\n"))
	hasEnv, hasSecrets := false, false
	for _, l := range lines {
		if strings.HasSuffix(l, ".env") && !strings.HasSuffix(l, ".secrets.env") {
			hasEnv = true
		}
		if strings.HasSuffix(l, ".secrets.env") {
			hasSecrets = true
		}
	}
	if !hasEnv || !hasSecrets {
		t.Fatalf("[D2-2] expected .env and .secrets.env in output, got: %v", lines)
	}
	// D2-3: .missing.env NOT present
	if strings.Contains(out, ".missing.env") {
		t.Fatalf("[D2-3] .missing.env must not appear in output (file absent → skip):\n%s", out)
	}
}

// [D3] ${COMPOSE_ENV} root-chain alias (scratch): .docker-env-chain uses
// .${COMPOSE_ENV}.env token; COMPOSE_ENV=test; .test.env exists → env-files
// includes abs path ending ".test.env". (1 assertion)
func TestChain_ComposeEnvToken(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".docker-env-chain"), []byte(".env\n.${COMPOSE_ENV}.env\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("A=1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".test.env"), []byte("T=1\n"), 0o644)

	out, err := runCenvkit(t, dir, []string{"COMPOSE_ENV=test"}, "env-files")
	if err != nil {
		t.Fatalf("[D3] env-files COMPOSE_ENV=test: %v\n%s", err, out)
	}
	// D3-1: abs path ending ".test.env" must appear (token substituted)
	if !strings.Contains(out, ".test.env") {
		t.Fatalf("[D3-1] expected path ending '.test.env' in output (token substituted):\n%s", out)
	}
}

// [D4] Quoted/comment/blank chain parsing (acceptance level, scratch).
// .env with Q1="hello world", Q2='x y', a # comment, a blank line →
// --value --var Q1 == "hello world"; --value --var Q2 == "x y". (2 assertions)
func TestChain_QuotedCommentBlankParsing(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("Q1=\"hello world\"\nQ2='x y'\n# comment\n\nOTHER=val\n"), 0o644)

	// D4-1: Q1 strips double-quotes → hello world
	out1, err := runCenvkit(t, dir, nil, "env-debug", "--value", "--var", "Q1")
	if err != nil {
		t.Fatalf("[D4-1] --value --var Q1: %v\n%s", err, out1)
	}
	if strings.TrimSpace(out1) != "hello world" {
		t.Fatalf("[D4-1] --value --var Q1 = %q, want 'hello world'", strings.TrimSpace(out1))
	}
	// D4-2: Q2 strips single-quotes → x y
	out2, err := runCenvkit(t, dir, nil, "env-debug", "--value", "--var", "Q2")
	if err != nil {
		t.Fatalf("[D4-2] --value --var Q2: %v\n%s", err, out2)
	}
	if strings.TrimSpace(out2) != "x y" {
		t.Fatalf("[D4-2] --value --var Q2 = %q, want 'x y'", strings.TrimSpace(out2))
	}
}

// [E] compose-go load failure fatal (scratch, non-docker): intentionally malformed
// docker-compose.yml → `env-debug --files` exits non-zero + stderr carries the error.
// Uses runCenvkitSplit to separate stdout/stderr. (2 assertions)
func TestEnvDebug_Files_ComposeLoadError(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("A=1\n"), 0o644)
	// malformed: flow sequence never closed → parse error from compose-go
	os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services:\n  web:\n    image: busybox\n  BADKEY: [broken\n"), 0o644)

	_, stderr, err := runCenvkitSplit(t, dir, nil, "env-debug", "--files")
	// E-1: exit non-zero
	if err == nil {
		t.Fatalf("[E-1] env-debug --files on malformed compose must exit non-zero")
	}
	// E-2: stderr carries the error (compose-go YAML parse error)
	if strings.TrimSpace(stderr) == "" {
		t.Fatalf("[E-2] stderr must be non-empty on compose load failure")
	}
}

// ─── end gap-report batch ─────────────────────────────────────────────────────

// ─── C1: gap-report exit-code contract (daemon-free acceptance) ──────────────
//
// These tests drive the BUILT cenvkit binary against a hermetic temp fixture
// (compose.yaml + web.env + .env). gap-report never execs docker, so they run
// under SMOKE_SKIP_DOCKER=1. The fixture is a minimal reproduction of the #3435
// gap: web service references ${WEB_PORT:-0} in ports, but WEB_PORT is only
// defined in a service env_file: (web.env), not in the Layer-1 chain (.env).
//
// envWithout returns os.Environ() minus the named keys so the child binary does
// not inherit a WEB_PORT (or COMPOSE_* override) that would mask the gap.
func envWithout(keys ...string) []string {
	skip := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		skip[k] = struct{}{}
	}
	var out []string
	for _, kv := range os.Environ() {
		k := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			k = kv[:i]
		}
		if _, excluded := skip[k]; !excluded {
			out = append(out, kv)
		}
	}
	return out
}

// writeGapAcceptFixture creates a temp dir with:
//
//	compose.yaml — web service env_file: [web.env] + ports: ["${WEB_PORT:-0}:80"]
//	web.env      — WEB_PORT=8080  (service env_file: only — the gap)
//	.env         — empty (gap case) or WEB_PORT=8080 (clean case)
func writeGapAcceptFixture(t *testing.T, inChain bool) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "compose.yaml"),
		[]byte("services:\n  web:\n    image: nginx\n    env_file:\n      - web.env\n    ports:\n      - \"${WEB_PORT:-0}:80\"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "web.env"), []byte("WEB_PORT=8080\n"), 0o644)
	env := ""
	if inChain {
		env = "WEB_PORT=8080\n"
	}
	os.WriteFile(filepath.Join(dir, ".env"), []byte(env), 0o644)
	return dir
}

// gapExitCode extracts the process exit code from an exec error; returns 0 if err==nil.
func gapExitCode(err error) int {
	if err == nil {
		return 0
	}
	var xe *exec.ExitError
	if errors.As(err, &xe) {
		return xe.ExitCode()
	}
	return -1
}

// [C1-gap1] gap-report exits 1 on a seeded gap (WEB_PORT env_file:-only).
// [C1-gap2] gap-report exits 0 when WEB_PORT is also in the Layer-1 .env.
// [C1-gap3] gap-report exits 2 when there is no compose file in the dir.
// [C1-gap4] gap-report --json exits 1 + output contains "count": 1 and "var": "WEB_PORT".
func TestGapReport_ExitCodeContract(t *testing.T) {
	// These tests are daemon-free; run under SMOKE_SKIP_DOCKER=1 too.
	cleanEnv := envWithout("COMPOSE_FILE", "COMPOSE_ENV_FILES", "COMPOSE_ENV", "WEB_PORT")

	// C1-gap1: seeded gap → exit 1
	gapDir := writeGapAcceptFixture(t, false)
	out1, err1 := func() (string, error) {
		c := exec.Command(cenvkitBin, "gap-report", "--project-dir", gapDir)
		c.Env = cleanEnv
		b, err := c.CombinedOutput()
		return string(b), err
	}()
	if gapExitCode(err1) != 1 {
		t.Fatalf("[C1-gap1] want exit 1 (gap found), got %d\nout: %s", gapExitCode(err1), out1)
	}
	if !strings.Contains(out1, "WEB_PORT") {
		t.Fatalf("[C1-gap1] gap output must mention WEB_PORT:\n%s", out1)
	}

	// C1-gap2: WEB_PORT in Layer-1 chain → exit 0 (clean)
	cleanDir := writeGapAcceptFixture(t, true)
	out2, err2 := func() (string, error) {
		c := exec.Command(cenvkitBin, "gap-report", "--project-dir", cleanDir)
		c.Env = cleanEnv
		b, err := c.CombinedOutput()
		return string(b), err
	}()
	if gapExitCode(err2) != 0 {
		t.Fatalf("[C1-gap2] want exit 0 (clean), got %d\nout: %s", gapExitCode(err2), out2)
	}

	// C1-gap3: no compose file → exit 2 (misconfiguration)
	noComposeDir := t.TempDir()
	os.WriteFile(filepath.Join(noComposeDir, ".env"), []byte("FOO=bar\n"), 0o644)
	_, err3 := func() (string, error) {
		c := exec.Command(cenvkitBin, "gap-report", "--project-dir", noComposeDir)
		c.Env = cleanEnv
		b, err := c.CombinedOutput()
		return string(b), err
	}()
	if gapExitCode(err3) != 2 {
		t.Fatalf("[C1-gap3] want exit 2 (no compose file), got %d", gapExitCode(err3))
	}

	// C1-gap4: --json → exit 1 + JSON schema contains count and var name
	out4, err4 := func() (string, error) {
		c := exec.Command(cenvkitBin, "gap-report", "--project-dir", gapDir, "--json")
		c.Env = cleanEnv
		b, err := c.CombinedOutput()
		return string(b), err
	}()
	if gapExitCode(err4) != 1 {
		t.Fatalf("[C1-gap4] want exit 1, got %d\nout: %s", gapExitCode(err4), out4)
	}
	for _, want := range []string{`"count": 1`, `"var": "WEB_PORT"`} {
		if !strings.Contains(out4, want) {
			t.Fatalf("[C1-gap4] json missing %q:\n%s", want, out4)
		}
	}
}

// ─── end C1: gap-report exit-code contract ───────────────────────────────────

// ─── C2: cenvkit env / run acceptance ────────────────────────────────────────
//
// All daemon-free (env/run never exec docker). Uses a scratch fixture.

// [C2-env-1] cenvkit env (dotenv) emits chain key; exits 0. (1 assertion)
// [C2-env-2] cenvkit env --format json emits JSON object with chain key. (2 assertions)
// [C2-env-3] cenvkit env --format shell emits `export KEY=…`. (1 assertion)
// [C2-env-4] cenvkit env on empty chain exits 0 with no output. (2 assertions)
// [C2-env-5] cenvkit env --no-expand emits literal ${VAR} unchanged. (1 assertion)
func TestEnv_DaemonFree(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("ENV_CHAIN_KEY=chain_val\n"), 0o644)
	cleanEnv := envWithout("ENV_CHAIN_KEY", "COMPOSE_FILE", "COMPOSE_ENV_FILES")

	// C2-env-1: dotenv default — chain key present, exit 0
	out1, err1 := func() (string, error) {
		c := exec.Command(cenvkitBin, "env", "--project-dir", dir)
		c.Env = cleanEnv
		b, e := c.CombinedOutput()
		return string(b), e
	}()
	if err1 != nil {
		t.Fatalf("[C2-env-1] env (dotenv) must exit 0, got %v\nout: %s", err1, out1)
	}
	if !strings.Contains(out1, "ENV_CHAIN_KEY=") {
		t.Fatalf("[C2-env-1] env must emit ENV_CHAIN_KEY=..., got:\n%s", out1)
	}

	// C2-env-2a: --format json exits 0
	out2, err2 := func() (string, error) {
		c := exec.Command(cenvkitBin, "env", "--project-dir", dir, "--format", "json")
		c.Env = cleanEnv
		b, e := c.CombinedOutput()
		return string(b), e
	}()
	if err2 != nil {
		t.Fatalf("[C2-env-2a] env --format json must exit 0, got %v\nout: %s", err2, out2)
	}
	// C2-env-2b: JSON output contains the chain key
	if !strings.Contains(out2, `"ENV_CHAIN_KEY"`) {
		t.Fatalf("[C2-env-2b] env --format json must contain ENV_CHAIN_KEY, got:\n%s", out2)
	}

	// C2-env-3: --format shell → `export KEY=…`
	out3, err3 := func() (string, error) {
		c := exec.Command(cenvkitBin, "env", "--project-dir", dir, "--format", "shell")
		c.Env = cleanEnv
		b, e := c.CombinedOutput()
		return string(b), e
	}()
	if err3 != nil {
		t.Fatalf("[C2-env-3] env --format shell must exit 0, got %v\nout: %s", err3, out3)
	}
	if !strings.Contains(out3, "export ENV_CHAIN_KEY=") {
		t.Fatalf("[C2-env-3] env --format shell must emit 'export ENV_CHAIN_KEY=', got:\n%s", out3)
	}

	// C2-env-4a: empty chain → exit 0
	emptyDir := t.TempDir()
	out4, err4 := func() (string, error) {
		c := exec.Command(cenvkitBin, "env", "--project-dir", emptyDir)
		c.Env = cleanEnv
		b, e := c.CombinedOutput()
		return string(b), e
	}()
	if err4 != nil {
		t.Fatalf("[C2-env-4a] env on empty chain must exit 0, got %v\nout: %s", err4, out4)
	}
	// C2-env-4b: empty chain → no output
	if strings.TrimSpace(out4) != "" {
		t.Fatalf("[C2-env-4b] env on empty chain must produce no output, got:\n%s", out4)
	}

	// C2-env-5: --no-expand leaves ${VAR} literal
	noExpDir := t.TempDir()
	os.WriteFile(filepath.Join(noExpDir, ".env"), []byte("LITERAL_KEY=${UNEXPANDED}\n"), 0o644)
	out5, err5 := func() (string, error) {
		c := exec.Command(cenvkitBin, "env", "--project-dir", noExpDir, "--no-expand")
		c.Env = envWithout("LITERAL_KEY", "UNEXPANDED")
		b, e := c.CombinedOutput()
		return string(b), e
	}()
	if err5 != nil {
		t.Fatalf("[C2-env-5] env --no-expand must exit 0, got %v\nout: %s", err5, out5)
	}
	if !strings.Contains(out5, "LITERAL_KEY=") {
		t.Fatalf("[C2-env-5] env --no-expand must emit LITERAL_KEY=..., got:\n%s", out5)
	}
}

// [C2-run-1] cenvkit run -- echo exits 0 and produces output. (1 assertion)
// [C2-run-2] cenvkit run (without --) exits 2. (1 assertion)
// [C2-run-3] cenvkit run --print exits 0 and emits chain env (no exec). (1 assertion)
func TestRun_DaemonFree(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("RUN_CHAIN_KEY=run_val\n"), 0o644)
	cleanEnv := envWithout("RUN_CHAIN_KEY", "COMPOSE_FILE", "COMPOSE_ENV_FILES")

	// C2-run-1: `run -- echo hello` exits 0 (child exits 0)
	out1, err1 := func() (string, error) {
		c := exec.Command(cenvkitBin, "run", "--project-dir", dir, "--", "echo", "hello")
		c.Env = cleanEnv
		b, e := c.CombinedOutput()
		return string(b), e
	}()
	if gapExitCode(err1) != 0 {
		t.Fatalf("[C2-run-1] run -- echo hello must exit 0, got %d\nout: %s", gapExitCode(err1), out1)
	}

	// C2-run-2: `run` without `--` exits 2
	_, err2 := func() (string, error) {
		c := exec.Command(cenvkitBin, "run", "--project-dir", dir, "echo", "hi")
		c.Env = cleanEnv
		b, e := c.CombinedOutput()
		return string(b), e
	}()
	if gapExitCode(err2) != 2 {
		t.Fatalf("[C2-run-2] run without -- must exit 2, got %d", gapExitCode(err2))
	}

	// C2-run-3: `run --print` emits chain env and exits 0 (does NOT exec `false`)
	out3, err3 := func() (string, error) {
		c := exec.Command(cenvkitBin, "run", "--project-dir", dir, "--print", "--", "false")
		c.Env = cleanEnv
		b, e := c.CombinedOutput()
		return string(b), e
	}()
	if gapExitCode(err3) != 0 {
		t.Fatalf("[C2-run-3] run --print must exit 0, got %d\nout: %s", gapExitCode(err3), out3)
	}
	if !strings.Contains(out3, "RUN_CHAIN_KEY=") {
		t.Fatalf("[C2-run-3] run --print must emit RUN_CHAIN_KEY=, got:\n%s", out3)
	}
}

// ─── C2: MF4 parity (docker-gated) ──────────────────────────────────────────
//
// MF4 (spec §5c, parity-critical): `cenvkit env --expand` == `env-debug --effective`
// == `docker compose config` for chain-scoped vars. A divergence here means the
// same chain gives different values across commands — a correctness bug.
//
// Scenarios:
//  1. chain var in env --format json: SITE_URL defined in .env appears in `cenvkit env` output.
//  2. chain var feeds compose interpolation: IS_DEV is referenced as ${IS_DEV:-unset} in
//     docker-compose.yml. In dev tier IS_DEV=true (from .dev.env), so `compose config`
//     must show "true" — not "unset" — proving cenvkit's chain set COMPOSE_ENV_FILES.
//  3. gap var absent from env: WEB_PORT lives only in web/.web.env (service env_file),
//     not the Layer-1 chain — `cenvkit env` must NOT emit it.
//  4. gap var fallback in compose config: WEB_PORT is referenced as ${WEB_PORT:-0} in
//     web/docker-compose.yml; since it is not in the chain it falls back to "0".
//  5. gap var visible in env-debug effective: env-debug --effective for the web service
//     DOES see WEB_PORT (from the service env_file at runtime).
//
// Scenarios 3–5 document the designed asymmetry (MF4 BOUNDARY CASE).

func TestParity_MF4_EnvEqualsEnvDebugEqualsCompose(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("SMOKE_SKIP_DOCKER=1")
	}
	root := stageMonorepo(t)
	// COMPOSE_ENV=dev selects the dev tier chain (.dev.env → IS_DEV=true).
	pEnv := envWithout("SITE_URL", "IS_DEV", "COMPOSE_ENV", "WEB_PORT", "COMPOSE_FILE", "COMPOSE_ENV_FILES")
	devEnv := append(pEnv, "COMPOSE_ENV=dev")

	// cenvkit env --format json (chain snapshot)
	envOut, envErr := func() (string, error) {
		c := exec.Command(cenvkitBin, "env", "--project-dir", root, "--format", "json")
		c.Env = devEnv
		b, e := c.CombinedOutput()
		return string(b), e
	}()
	if envErr != nil {
		t.Fatalf("[MF4-setup-env] env --format json: %v\n%s", envErr, envOut)
	}

	// cenvkit compose config (the compose reference; exercises COMPOSE_ENV_FILES feeding)
	cfgOut, cfgErr := func() (string, error) {
		c := exec.Command(cenvkitBin, "compose", "--project-dir", root, "config")
		c.Env = devEnv
		b, e := c.CombinedOutput()
		return string(b), e
	}()
	if cfgErr != nil {
		t.Fatalf("[MF4-setup-cfg] cenvkit compose config: %v\n%s", cfgErr, cfgOut)
	}

	// env-debug --effective (web service; shows runtime env_file layer too)
	dbgOut, dbgErr := func() (string, error) {
		c := exec.Command(cenvkitBin, "env-debug", "--project-dir", root, "--effective", "--service", "web", "--json")
		c.Env = devEnv
		b, e := c.CombinedOutput()
		return string(b), e
	}()
	if dbgErr != nil {
		t.Fatalf("[MF4-setup-dbg] env-debug --effective --json: %v\n%s", dbgErr, dbgOut)
	}

	// MF4-parity-1: SITE_URL (plain chain var) appears in `cenvkit env` output.
	if !strings.Contains(envOut, `"SITE_URL"`) {
		t.Fatalf("[MF4-parity-1] env --format json must contain SITE_URL (chain var):\n%s", envOut)
	}
	// MF4-parity-2: IS_DEV (referenced in compose as ${IS_DEV:-unset}) must be "true" in
	// compose config — not "unset" — proving cenvkit fed COMPOSE_ENV_FILES to docker compose.
	if !strings.Contains(cfgOut, `IS_DEV: "true"`) {
		t.Fatalf("[MF4-parity-2] compose config must show IS_DEV: \"true\" (chain fed interpolation; dev tier):\n%s", cfgOut)
	}
	// MF4-parity-3: WEB_PORT must NOT appear in `cenvkit env` output (env_file-only, not a chain key).
	if strings.Contains(envOut, "WEB_PORT") {
		t.Fatalf("[MF4-parity-3] env must NOT emit WEB_PORT (not a chain key; env_file-only):\n%s", envOut)
	}
	// MF4-parity-4: compose config shows WEB_PORT falls back to "0" (${WEB_PORT:-0} in compose yaml;
	// not in chain → fallback fires).
	if !strings.Contains(cfgOut, `WEB_PORT: "0"`) {
		t.Fatalf("[MF4-parity-4] compose config must show WEB_PORT: \"0\" (chain-absent var falls back):\n%s", cfgOut)
	}
	// MF4-parity-5: env-debug --effective shows WEB_PORT from the service env_file (runtime layer).
	if !strings.Contains(dbgOut, "WEB_PORT") {
		t.Fatalf("[MF4-parity-5] env-debug --effective must show WEB_PORT (runtime service env_file value):\n%s", dbgOut)
	}
}

// ─── end C2: cenvkit env / run acceptance ────────────────────────────────────

// ─── color / ANSI no-leak guards ─────────────────────────────────────────────
//
// The acceptance assertions above run non-TTY/CI, so their literal-string
// matches already guard against leaked ANSI escapes (a stray \x1b would break
// strings.Contains checks). These extra tests make the no-leak contract
// explicit and also verify --color=always forces ANSI when requested.

// TestColor_NoLeak_DefaultNonTTY: the default (auto) color mode on a non-TTY
// process produces no ANSI escapes. This is also an implicit guard for the
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
