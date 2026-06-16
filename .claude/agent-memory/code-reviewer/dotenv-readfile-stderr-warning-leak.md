---
name: dotenv-readfile-stderr-warning-leak
description: dotenv.ReadFile(path, nil) leaks unset-var stderr warnings via internal template.Substitute (no WithoutLogging); fix = pass chainEnv lookupFn
metadata:
  type: project
---

`dotenv.ReadFile(path, lookupFn)` (compose-go v2.11.0) internally calls
`dotenv.expandVariables` → `template.Substitute(...)` (NOT `SubstituteWithOptions`
+ `WithoutLogging`) for any in-file `${VAR}` reference. So a nil lookupFn makes
every in-file ref that isn't resolved emit
`level=warning msg="The \"X\" variable is not set..."` to stderr.

**Why:** the provenance v2 plan's A (chain attribution) and C (per-service env)
paths call `parseDotEnv = dotenv.ReadFile(path, nil)`, while the parallel B-lite
leaf path uses `template.SubstituteWithOptions(leaf, mapping, WithoutLogging)`
(plan line 493) — an inconsistency. Live-probe verified (CASE 1): nil lookup
warns for BOTH chain-set and genuinely-unset in-file refs.

**How to apply:** the warning is NOT silenceable by any option on ReadFile — the
ONLY caller-side lever is to make the var resolve. Pass a non-nil LookupFn built
from the merged chainEnv (the same map used as the interpolation `mapping`) to
`dotenv.ReadFile`. Live-probe CASE 2/3: this quiets warnings for chain-set vars;
a genuinely-unset external still warns (correct docker-compose parity — keep it).
chainEnv/mapping are in scope at both A (plan ~396) and C (~444) call sites, so
the fix is wireable. Relates to [[carried-bug-classes-cenvkit]] (no secrets/noise
to logs) and the WithoutLogging discipline in
[[provenance-mapping-missing-layer2]].
