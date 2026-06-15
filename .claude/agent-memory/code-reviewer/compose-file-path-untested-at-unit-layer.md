---
name: compose-file-path-untested-at-unit-layer
description: cenvkit engine unit tests all use default discovery; the interpolated COMPOSE_FILE overlay path has NO docker-free guard, and scenario 15 (its only acceptance pin) is docker-skippable
metadata:
  type: project
---

The cenvkit Task 3 engine test suite (TestResolve_MissingRequiredEnvFile,
TestResolve_Deterministic, TestResolve_MonorepoFixture_CrossSubproject) all
exercise ONLY default compose-file discovery. None sets COMPOSE_FILE or a
COMPOSE_ENV=prod interpolated overlay. So the two critical COMPOSE_FILE seam
defects (see [[compose-go-option-order-and-compose-file]]) — WithEnv must precede
WithConfigFileEnv, and COMPOSE_FILE's ${COMPOSE_ENV} is NOT interpolated by
compose-go — have NO docker-free guard.

The plan's claimed acceptance pin for the overlay (smoke-monorepo scenario 15,
line 598) is gated `if [ "$HAVE_DOCKER" -eq 1 ]` and ported with
`if !dockerAvailable() { t.Skip() }` (Task 7 Step 5). Under SMOKE_SKIP_DOCKER=1
it is SKIPPED — so the COMPOSE_FILE path can be green at unit AND skipped at
acceptance simultaneously.

**Why:** This is a guard-validity hole, not just missing coverage — a defect on
the COMPOSE_FILE path would ship green in any docker-less CI/dev run.

**How to apply:** Demand a docker-FREE engine-level RED test that loads via an
interpolated COMPOSE_FILE overlay and asserts on res.EnvFiles (NOT STACK_TIER —
the engine returns env_file paths, not compose-config values). NOTE: the existing
examples/monorepo overlays differ only by `environment: STACK_TIER`, NOT by
`env_file:` — so the test needs its OWN scratch fixture where the prod overlay
re-opens a service to add a DISTINCT env_file. The test must be RED on the plan's
current config-first option order (temp-revert check). Relates to
[[carried-bug-classes-cenvkit]] (seam-drift / guard-from-birth class).
