---
name: secrets-last-scope
description: "secrets-last" invariant is within Layer-1 only; merged COMPOSE_ENV_FILES has Layer-2 after Layer-1
metadata:
  type: project
---

The "secrets last" contract (W3) is scoped to Layer-1 only: `.secrets.env` is the last entry in the Layer-1 chain. The full merged COMPOSE_ENV_FILES list has Layer-2 service env files AFTER all of Layer-1 (correct and intended).

**Why:** `env-files` output shows the merged list (Layer-1 + Layer-2). Checking `lines[len(lines)-1]` for `.secrets.env` will fail when Layer-2 files exist. The correct assertion for 12.4 is to use `env-debug --chain` (which outputs Layer-1 only) and verify `.secrets.env` is last there.

**How to apply:** In acceptance tests that check secrets ordering, use `cenvkit env-debug --chain` output, not `cenvkit env-files` output, to assert secrets-last-in-chain.
