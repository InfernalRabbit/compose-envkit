---
name: provenance-cpass-noop-option-omission
description: Provenance C-pass omitting WithConfigFileEnv/WithDefaultConfigPath is NOT divergence from Resolve — both are no-ops once ConfigPaths is set (probe-grade source proof)
metadata:
  type: project
---

The v2 provenance C-pass (`cli.NewProjectOptions(configs, ...)`) omits
`cli.WithConfigFileEnv` + `cli.WithDefaultConfigPath` that v1 `engine.Resolve`
includes. Reviewers flag this as "option-set divergence → env_file paths could
differ." That finding is **real=false**.

**Why:** compose-go v2.11.0 `cli/options.go` — `NewProjectOptions(configs,...)`
sets `options.ConfigPaths = configs` (line 93) BEFORE any option fn runs. Both
`WithConfigFileEnv` (line 138) and `WithDefaultConfigPath` (line 156) open with
`if len(o.ConfigPaths) > 0 { return nil }` — strict no-ops once configs are set.

In BOTH passes the configs list is computed by the SAME shared helper from the
SAME inputs: `resolveComposeFiles(in.ProjectDir, in.Env)` (engine.go:56 ==
provenance plan line 390). And both passes only reach the loader when
`len(configs) > 0` (provenance early-returns chain-only at plan lines 414-417 if
empty). So the two omitted options are unreachable code in both passes; the
effective ConfigPaths — and thus every env_file path — is provably identical.
Only the dead options differ.

**How to apply:** Do NOT score "C-pass omits WithConfigFileEnv/WithDefaultConfigPath"
or "C-load vs Resolve-load option-set drift" as a defect. The drift the finding
fears is already structurally prevented by the shared `resolveComposeFiles`
helper. Confirms the [[provenance-plan-double-load-is-seam-intentional]] theme:
the two loads are deliberately separate but share the config-derivation seam.
Related: [[withconfigfileenv-no-interp-cwd]], [[compose-go-option-order-and-compose-file]].
