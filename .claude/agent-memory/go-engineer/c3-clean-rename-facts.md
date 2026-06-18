---
name: c3-clean-rename-facts
description: C3 clean-break rename (.docker-env-chain→.cenvkit.envchain, COMPOSE_ENV→CENVKIT_ENV) blast radius + the COMPOSE_ENV-injection crux + decision A
metadata:
  type: project
---

Cycle 3 = CLEAN BREAK, NO back-compat aliases (user directive 2026-06-19 overrides
spec §7 which still says aliases). cenvkit pre-1.0, no external users.

**Why:** user wants the chain file `.cenvkit.envchain` and selector `CENVKIT_ENV`
to be the ONLY names — `.docker-env-chain` and `COMPOSE_ENV` STOP being read, no
fallback.

**How to apply:** two prod files do the chain-reading rename:
- `internal/chain/chain.go`: `readChainTemplates` opens `.docker-env-chain`
  (~:131,136,149) → `.cenvkit.envchain`; `resolveComposeEnv` reads `COMPOSE_ENV`
  shell+`.env` (~:93,98) → `CENVKIT_ENV`; the var INJECTED into merged Vars
  (~:200-201) → `CENVKIT_ENV`; `${COMPOSE_ENV}` template token in `substituteTokens`
  (~:123) + `defaultChain` (~:32) KEEP working (`${ENV}`/`${CENVKIT_ENV}` both
  substitute the resolved tier — drop the `${COMPOSE_ENV}` token alias).
- `internal/engine/discover.go`: `interpolateComposeFile` seedLookup "COMPOSE_ENV"
  (~:28) + `${COMPOSE_ENV}` replacer (~:29) → CENVKIT_ENV / `${CENVKIT_ENV}`.
- `cmd/cenvkit/main.go`: every `"COMPOSE_ENV="+env` injection (~:156,288,302) is
  cenvkit's selector → `CENVKIT_ENV=`; flag help "overrides COMPOSE_ENV" (~:498,543).
- `internal/provenance/render.go`: `--overview` header label `COMPOSE_ENV = %s`
  (~:219,221) → human-output change → SEQUENCE with qa (output-changing edit rule).

**THE CRUX (resolved → option A):** cenvkit INJECTS its selector into merged Vars
(chain.go:201) so `${COMPOSE_ENV}` interpolates in COMPOSE_FILE + compose YAML.
`COMPOSE_ENV` is NOT a real docker-compose var (cenvkit's own, named compose-ish),
so renaming does NOT break compose. Decision A (matches no-fallback): inject
`CENVKIT_ENV` only + migrate fixtures' `${COMPOSE_ENV}` tokens → `${CENVKIT_ENV}`.

**CRITICAL non-touch:** `COMPOSE_ENV_FILES` is a REAL docker-compose var — NEVER
rename it. Only `COMPOSE_ENV` (bare) is cenvkit's selector.

**Fixture facts:** root `.env`/`.dev.env`/`.prod.env` are GITIGNORED (seeded from
`example.*` by `cenvkit init`; bootstrap.go is a generic example.X→.X copier with
NO chain/COMPOSE_ENV refs → needs ZERO changes). Committed load-bearing token
sources: `example.env` (`COMPOSE_ENV=dev` selector line + `COMPOSE_FILE=...${COMPOSE_ENV}.yml`),
`web/docker-compose.yml:26` (`./.web.${COMPOSE_ENV:-dev}.env`). `.docker-env-chain`
is committed directly (not seeded) → rename the file itself (qa zone).

**qa contract changes (I list, qa edits):** `test/seam_test.go:81-89` (cr.Vars must
emit CENVKIT_ENV=dev), acceptance `--value --var COMPOSE_ENV`→`CENVKIT_ENV` (:594),
`--overview` header assertion `render_test.go:243` + acceptance overview tests, all
`{"COMPOSE_ENV=..."}` run-env injections in acceptance → `CENVKIT_ENV=`.

**LATENT TEST WEAKNESS (found at freeze):** `render_test.go:242` asserts
`strings.Contains(got, "COMPOSE_ENV")`. After the header label flips to
`CENVKIT_ENV =`, this STILL PASSES — but only incidentally, because the test's
own fixture (`:124`) seeds a Layer-1 chain var literally named `COMPOSE_ENV` that
renders in the overview BODY (`+ COMPOSE_ENV = dev`). The header-label change is
masked. qa must flip BOTH `:124` (the fixture var) AND `:242` to `CENVKIT_ENV`,
else the test green-washes a label it no longer checks. New verified header text:
`  CENVKIT_ENV = dev (from .env)` / `  CENVKIT_ENV = prod (from shell)`;
`Interpolation chain (COMPOSE_ENV_FILES)` section title KEEPS the real var name.

**Freeze gate (verified):** gofmt -l . empty, go build/vet clean. Unit fails are
ONLY old-name tests in chain + engine pkgs (qa-awaiting): TestResolveComposeEnvSource,
TestChainOrderingAndEnvSwitch, TestCommaInComposeEnv_DoesNotSplit,
TestHostSanitization/TestHostTokenEqualsHostnameToken (write .docker-env-chain),
engine TestHasComposeFile/TestResolve_InterpolatedComposeFileOverlay (${COMPOSE_ENV}).

Related: [[v3-a-attribution-two-env-trap]] (interp env threading),
[[monorepo-fixture-layer1-needs-seeding]] (gitignored seed copies).
