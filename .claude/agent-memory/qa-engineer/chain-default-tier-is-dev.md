---
name: chain-default-tier-is-dev
description: When COMPOSE_ENV is unset and .env has no COMPOSE_ENV= line, chain.Resolve defaults to tier "dev" — .dev.env is ALWAYS loaded unless you specify a different tier
metadata:
  type: feedback
---

The chain resolver (internal/chain/chain.go) defaults `COMPOSE_ENV` to `"dev"` when:
- `COMPOSE_ENV` is absent from `os.Environ()` (or empty), AND
- the project's `.env` has no `COMPOSE_ENV=` line

This means `.dev.env` is loaded by default in test fixtures that have no `COMPOSE_ENV` set.

**Why:** A test for `-e <env>` flag re-resolution used `.dev.env` + `DEV_KEY` expecting DEV_KEY to be absent without `-e dev`. It failed because `dev` is the default tier — DEV_KEY was always loaded.

**How to apply:** When testing `-e <tier>` flag behavior, use a NON-default tier name (e.g. `staging`, `prod`, `canary`) with a tier-specific key. Do NOT use `.dev.env` to test "absent without flag" behavior. See `TestEnvCmd_Env_Flag_ReResolves` which uses `.staging.env` + `STAGING_KEY`. [[feedback-tsetenv-vs-unsetenv]]
