---
name: provenance-chain-vs-files-view
description: env-debug --chain must render Layer-1 only (secrets-last contract); the rich-provenance plan's RenderHuman collapsed --chain and --files to the same full Report.Files list — a regression caught by acceptance 12.4
metadata:
  type: feedback
---

`env-debug --chain` (and the default view) MUST print the **Layer-1-only** file
list; `env-debug --files` prints the **full merged** COMPOSE_ENV_FILES (Layer-1 +
Layer-2). The v1 sh kit + v1 Go (`debug.PrintChain(cr.Files)` vs
`debug.PrintFiles(merged)`) kept these distinct, and acceptance assertion 12.4
("secrets must be last in Layer-1 chain", `test/cenvkit-acceptance_test.go`)
pins it by driving `env-debug --chain` and checking the LAST line is `.secrets.env`.

**Why:** The 2026-06-16 rich-provenance plan's `RenderHuman` sketch had BOTH the
`o.Files` case and the `default` (Chain) case iterate `r.Files` — but
`provenance.Report.Files` is the FULL merged list (engine appends every
`in.EnvFiles` entry, Layer-1 then Layer-2). So copying the plan verbatim made
`--chain` show Layer-1+Layer-2, pushing the deeper env-chain files after
`.secrets.env` and breaking 12.4. The model has no Layer-1 subset on `Files`
(it's a flat ordered list by design; per-file layer lives in
`Vars[*].Winner.Layer`, not on a file).

**How to apply:** The FINAL chosen design (lead-directed) is a `ChainFiles
[]string \`json:"chain_files"\`` field on `provenance.Report` (NOT on `HumanOpts`),
populated by the ENGINE in the A-attribution loop of
`internal/engine/provenance.go` (`if f.Layer == "layer1" { rep.ChainFiles =
append(rep.ChainFiles, f.Path) }`, placed before the missing-file `continue` and
the chain-only early-return so the listing is consistent and populated in
chain-only mode). `Report.Files` stays the full merged list; `render.go` `--chain`
+ default iterate `r.ChainFiles`, `--files` iterates `r.Files`. This gives JSON
parity (Layer-1 visible in `--json` too) — better than my interim
`HumanOpts.ChainFiles` (render-only) approach, which I removed.

General lessons: (1) The plan's code blocks are probe-grounded for the compose-go
mechanism but NOT for every CLI view's contract — run the existing acceptance
suite (esp. ordering/secrets-last) before claiming a cmd rewire done; a verbatim
copy of a render sketch can silently drop a v1 view distinction (`--chain` ==
Layer-1 only vs `--files` == full merged). (2) VERIFY-BEFORE-CLAIM caught a
half-applied multi-zone fix: a 3-step change (model + render + engine) was
committed with only 2 steps landed (engine population missing), so `--chain`
printed `[]` and [12.4] was RED at a HEAD reported "green". grep the committed tree
for the field, and run the actual failing test, before trusting a "committed +
green" claim. (3) ZONE: per CLAUDE.md, go-engineer owns ALL of `internal/**`
INCLUDING `internal/engine/`; the plan-mode gate applied ONLY to the seam-CONTRACT
change (adding a method to the `Engine` interface), not to trivial field
population. Related: [[cobra-persistent-flag-read-from-root]].
