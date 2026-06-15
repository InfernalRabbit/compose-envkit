---
name: env-debug-layer-scope-per-mode
description: env-debug modes have DIFFERENT layer scopes — --value is Layer-1-only, --trace is Layer-2-rooted; do not collapse them
metadata:
  type: project
---

cenvkit `env-debug` modes do NOT share a single layer scope. Verifying any
finding that says "pass the same file list to all modes" requires per-mode checks
against the legacy `lib/env-debug.sh`.

- `--value`: Layer-1 ONLY (project chain). Legacy `env-debug.sh:111-117,133`
  sources `_CHAIN_FILES` (.docker-env-chain / defaults), explicitly NOT container
  env_file: (Layer-2). Reason: Layer-2 holds bare `${...}` compose refs unsafe to
  shell-source + secret-scope. smoke.sh:218 asserts "--value sources ONLY the
  project chain". Passing the full merged list (Layer-1+Layer-2) is a real defect
  (Layer-2 last-wins shadows Layer-1; can surface a secret).
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
