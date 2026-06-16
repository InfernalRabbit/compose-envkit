---
name: env-debug-layer-scope-per-mode
description: env-debug modes have DIFFERENT layer scopes — --value is Layer-1-only, --trace is Layer-2-rooted; do not collapse them
metadata:
  type: project
---

cenvkit `env-debug` modes do NOT share a single layer scope. Verifying any
finding that says "pass the same file list to all modes" requires per-mode checks
against the legacy `lib/env-debug.sh`.

- `--value` (v1 LEGACY): Layer-1 ONLY (project chain). Legacy
  `env-debug.sh:111-117,133` sources `_CHAIN_FILES` (.docker-env-chain /
  defaults), explicitly NOT container env_file: (Layer-2). Reason: Layer-2 holds
  bare `${...}` compose refs unsafe to shell-source + secret-scope. smoke.sh:218
  asserts "--value sources ONLY the project chain".
- `--value` (v2 rich-provenance): DELIBERATELY BROADENED to the merged
  COMPOSE_ENV_FILES winner (Layer-1+Layer-2 last-wins). This is SPEC-SANCTIONED,
  not a defect: v2 SPEC §1 (lines 12,21 "supersedes v1's raw --value/--trace, a
  v1 narrowing"), §2-A (line 27 winner over "merged COMPOSE_ENV_FILES"), §7
  (lines 164-165 "--value VAR → winning value from provenance (replaces v1's
  raw-merge --value)"). Plan T4 Step 1 (line 703) updates the acceptance assertion
  ("replaces the v1 raw assertion") and Step 2 requires lead sign-off on the new
  count. So a v2 finding that calls the merged-list --value a "silent Layer-2 leak
  regression contradicting spec" is FALSE — the change is declared in spec §7 and
  test-updated with sign-off; the finding's own escape clause is already met.
  CAVEAT (legit but separate): broadening --value can surface a Layer-2 SECRET via
  last-wins. That is a substantive safety concern to RAISE as a Warning (and §6
  emit order Layer-1-then-Layer-2 means a colliding Layer-2 file lands after
  .secrets.env), but it is NOT a "plan is broken / contradicts spec" critical.
  KEY for v1: passing the full merged list to v1 --value WOULD be a real defect
  (undeclared). The trap is verifying legacy scope but not re-checking whether the
  v2 spec deliberately supersedes it.
- `--trace`: Layer-2 ROOTED. Legacy `trace_mode` (env-debug.sh:605-704) iterates
  ACTIVE_SERVICES, finds the var in container env_file: (Layer-2) at line 628,
  THEN resolves `${...}` refs into the project chain (Layer-1) at line 666. So
  trace spans BOTH layers, starting in Layer-2. smoke.sh fixture (lines 12,69)
  defines SVC_PORT ONLY in svc.env (a Layer-2 env_file) and smoke.sh:213 runs
  `--trace --var SVC_PORT`. Passing Layer-1-only to Trace would make it find
  NOTHING — breaks the smoke trace assertion.

How to apply: when a consistency finding lumps --value and --trace together,
split them. The merged list is wrong for --value; Layer-1-only is wrong for
--trace. See [[plan-consistency-defect-classes]] (the --value Layer-2 leak is one
of the known recurring plan defects).
