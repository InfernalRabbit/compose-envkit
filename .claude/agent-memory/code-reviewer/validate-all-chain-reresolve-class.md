---
name: validate-all-chain-reresolve-class
description: cenvkit `validate --all` must re-resolve the Layer-1 chain per env (COMPOSE_ENV in chain Input), not just on the docker subprocess — legacy DC_PROD proves it
metadata:
  type: project
---

`cenvkit validate --all` (and any per-env operation) must inject the target
`COMPOSE_ENV` into the **chain resolution** (`chain.Input.OSEnv`), not only onto
the `docker compose config -q` subprocess env. Setting `COMPOSE_ENV=prod` only on
the subprocess leaves `assemble()` resolving the chain from `os.Environ()` (env
unset → "dev"), so `.${COMPOSE_ENV}.env` expands to `.dev.env` and the prod run
validates the DEV chain under a prod label — incoherent, and a parity break.

**Why:** legacy `mk/compose.mk:43` `DC_PROD ?= COMPOSE_ENV=prod ./docker compose`
sets COMPOSE_ENV in the shim's OWN environment BEFORE chain assembly. The shim
(`lib/compose-env.sh:45` `ENV=${COMPOSE_ENV:-${_FILE_ENV:-dev}}`, `:75` substitutes
`${COMPOSE_ENV}` in chain templates) therefore picks `.prod.env`. Go mirror is
`internal/chain` `resolveComposeEnv`/`substituteTokens` reading COMPOSE_ENV from
`osEnv`. Spec S3 (design §188-190) wants --all to validate dev AND prod.

**How to apply:** when reviewing per-env commands, check that the env string flows
into the chain `Input.OSEnv` (appended AFTER os.Environ() so it wins last via
osEnvMap's last-wins), not merely the exec'd subprocess. The RED guard: assert the
prod run's COMPOSE_ENV_FILES contains `.prod.env` and NOT `.dev.env`. Related:
[[env-debug-layer-scope-per-mode]], [[compose-go-option-order-and-compose-file]].

**Citation correction (verified 2026-06-15):** the parity reference is legacy
`mk/compose.mk:65-67` (`validate: dev + prod` via `DC_PROD ?= COMPOSE_ENV=prod
./docker compose`, line 43) and the `smoke.sh:252` `make validate` check — NOT
smoke-monorepo scenario 22, which is "Submodule shape." Note the env-switching
acceptance scenarios (8/15/16/17) all `export COMPOSE_ENV=prod` in the AMBIENT
shell, so os.Environ() carries it and the chain resolves correctly there; the bug
is unique to `validate --all` because it switches env via an INTERNAL run(env)
parameter that never reaches assemble()/chain.Resolve. So a naive port can pass
8/15/16/17 yet still ship the broken --all — the guard must drive --all directly.
