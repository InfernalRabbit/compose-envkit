---
name: provenance-mapping-missing-layer2
description: cenvkit v2 provenance B-lite/C interpolation mapping is seeded from cr.Vars (Layer-1 only) — misses Layer-2 service env_file vars that ARE the product's reason to exist
metadata:
  type: project
---

The 2026-06-16 rich-provenance PLAN seeds the interpolation mapping for B-lite
(and the C resolved-load) from `in.Env` == `cr.Vars` == chain Layer-1 only. But
cenvkit's whole point is that Layer-2 service `env_file:` vars (e.g. `WEB_PORT`
in `web/.web.env`) feed compose `${VAR}` interpolation. `cr.Vars` does NOT
contain Layer-2 vars (chain.Resolve never reads service env_files).

Consequence: `template.SubstituteWithOptions("${WEB_PORT:-0}:80", mapping)` with
mapping=Layer-1 resolves WEB_PORT unset → "0:80", not "18080:80". The probe
passed ONLY because it injected WEB_PORT directly into the env slice; the real
monorepo defines it Layer-2-only. Plan Task 3 smoke + Task 4 acceptance assert
the real resolved port → they FAIL as written.

Fix: the interpolation mapping must be the MERGED COMPOSE_ENV_FILES env
(Layer-1 ∪ Layer-2, last-wins), i.e. parse all `in.EnvFiles` into the chainEnv
(in order) THEN overlay `in.Env` (OS-wins), and use THAT as both `template.Mapping`
and `details.Environment` and `cli.WithEnv`. This mirrors what `docker compose`
does (reads COMPOSE_ENV_FILES before interpolation).

RE-VERIFIED 2026-06-16 against current plan + live module (v2.11.0):
cmd Task 3 plan:636 passes `Env: cr.Vars` (Layer-1 only) to Provenance; the
`EnvFiles: pf` it passes carries layer1+layer2 PATHS but those only feed
A-attribution (rep.Vars), never the B-lite `mapping`. chain.go confirms
chain.Resolve reads ONLY the .docker-env-chain templates (.env/.${ENV}.env/
.${HOSTNAME}.env/.secrets.env) + OS env — never service env_files, so cr.Vars
lacks WEB_PORT. Live test: SubstituteWithOptions("${WEB_PORT:-0}:80",
layer1-only, WithoutLogging) => "0:80"; merged => "18080:80". Finding is REAL
(major). Note the Task 2 UNIT test (plan:300) injects WEB_PORT=8080 into env so
it stays green — the gap only surfaces in Task 3 smoke (plan:685) + Task 4
acceptance (plan:700-701) driving the staged monorepo where WEB_PORT is
Layer-2-only.

Also: A-attribution `Value` is taken from raw `dotenv.Parse(file)`, ignoring the
OS-env overlay that chain applies (OS wins). So the reported winner Value can
disagree with the value actually used for B-lite interpolation when a var is also
exported in the shell.

**Why:** recurring class — a probe fixture that pre-seeds the var under test masks
a real wiring gap (the var's true home is a layer the code never reads).

**How to apply:** when reviewing any "resolve ${VAR}" step, trace WHERE the
mapping's values come from vs WHERE the test var actually lives. If the test fixture
injects the var directly but production sources it from a different layer, the
mapping is probably under-seeded. Relates to [[env-debug-layer-scope-per-mode]]
and [[provenance-plan-spec-drift]].
