---
name: tsetenv-vs-unsetenv-gap-hermeticity
description: t.Setenv("K","") does NOT unset the key — it leaves it in os.Environ() as K=, which makes the engine see it as InChain=true and masks gaps
metadata:
  type: feedback
---

`t.Setenv("WEB_PORT", "")` sets the key to an empty string; it remains present in `os.Environ()`. The engine at `internal/engine/provenance.go:179` overlays `in.Env` (= `os.Environ()` via `chain.Resolve`) into `interpEnv`, so `_, ok := interpEnv["WEB_PORT"]` returns `ok=true` → `InChain=true` → no gap detected. Tests that called `t.Setenv` to "clear" gap-related vars passed clean when they should have returned exit 1.

**Why:** `t.Setenv` was designed to set-with-cleanup, not unset. Setting to `""` still populates the var in the process env.

**How to apply:** For in-process cmd tests, use `os.Unsetenv(k)` with a `t.Cleanup` restore to actually remove the key from the process env. For out-of-process acceptance tests (binary invocation), build an explicit env slice via a helper like `envWithout(keys...)` that filters `os.Environ()` before passing to `exec.Command.Env`. See `cmd/cenvkit/main_test.go:clearGapEnv` and `test/cenvkit-acceptance_test.go:envWithout`.
