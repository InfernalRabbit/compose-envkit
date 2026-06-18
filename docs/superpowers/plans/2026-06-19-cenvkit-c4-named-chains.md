# cenvkit C4 — named chains (`[name]` sections + `--chain <name>`)

> Authored by the architect from the C4 plan-gate (approved 2026-06-19). Additive flexibility: standalone named sections, NO inheritance, NO compose binding. PROD = go-engineer; `*_test.go` + fixtures = qa; docs + git = architect.

**Goal:** Let `.cenvkit.envchain` carry optional INI `[name]` sections; `--chain <name>` selects one (default `[default]` / header-less). Orthogonal to `CENVKIT_ENV`.

## Decisions (approved 2026-06-19)
- **Collision = (a):** rename `env-debug`'s boolean `--chain` mode → **`--list`**; `--chain <name>` (string) becomes the ONE universal selector. (`env-debug --chain` is the default-view bool, == bare `env-debug`, render.go:116 — nearly redundant; the internal `HumanOpts.Chain`/`Report.ChainFiles` VIEW selector stays, only the cobra flag string changes.) **Breaking CLI-surface change → CHANGELOG note (architect).**
- **Scope finding:** named chains only change WHICH list `chain.Resolve` returns (via the existing `Result.Files`, chain.go:24). `internal/engine` / `internal/envmap` / `internal/envfiles` need **ZERO changes** (they consume `cr.Files`/`cr.Vars`). All work = `internal/chain` + cmd flags.
- **`--chain` flag shape:** PREFERRED = a PERSISTENT root flag mirroring `--project-dir` (main.go:56-57) + a `resolveChainName(cmd)` helper (mirrors `resolveProjectDir`), rather than 8 per-command flags. (go-engineer's final call, justified in code.)

## PROD (go-engineer)
- **T1 — `internal/chain`:** add `Chain string` to `chain.Input` (empty ⇒ `"default"`); `readChainTemplates` (chain.go:130) parses INI `[name]` headers and selects the requested section; lines before any header (or a header-less file like the committed `examples/monorepo/.cenvkit.envchain`) = implicit `[default]`; sections are **standalone, no inheritance**; a new error type carries the available section names; `Resolve` (chain.go:155) threads `in.Chain`. `Result` gains nothing.
- **T2 — `cmd/cenvkit`:** wire `--chain <name>` (persistent preferred) → `chain.Input.Chain` via `assemble()` (main.go:122) + `resolvePopulator()` (main.go:149); available on `run`/`env`/`env-files`/`validate`/`gap-report`/`env-debug`. Rename `env-debug`'s bool `--chain`→`--list` (flag string only; HumanOpts.Chain stays). `compose` (DisableFlagParsing, main.go:219): generalize `extractProjectDir` (main.go:258) to ALSO pre-scan + STRIP `--chain` before forwarding to docker.
- **Errors:** `--chain <name>` absent → `exitError{code:2}` + message listing available names; header-less file ⇒ only `default`; `--chain default` always valid.

## QA (`*_test.go` + fixtures)
- chain unit: header-less ⇒ default; `[name]` standalone (no inheritance); missing section → error with names; `default` valid both ways; `${CENVKIT_ENV}`/`${HOST}` tokens still substitute within a chosen section (orthogonality).
- cmd/acceptance: `--chain api` picks a different list across run/env/env-files; `--chain` on `compose` is stripped (not leaked to docker); missing-section → exit 2 + names; `env-debug --list` == bare default view (flag-rename regression). Add a multi-section fixture.
- **Bump `declaredAssertions`** (test/cenvkit-acceptance_test.go:63) AND the line-2 header together — `TestAssertionCountHeader` (:68) enforces both.

## Docs (architect)
CHANGELOG `[Unreleased]`: ⚠ BREAKING (pre-1.0) — `env-debug`'s `--chain` mode flag renamed to `--list`; new `--chain <name>` selects a named section of `.cenvkit.envchain` (default = the header-less / `[default]` list).

## Sequencing
The `env-debug --chain`→`--list` rename is an output/CLI-surface change qa asserts on → go-engineer lands + freezes it + reports the exact new flag set FIRST; THEN qa writes tests. The rest (parser + `--chain` wiring) is additive. Architect: full gate on the frozen tree → code-review → squashed C4 commit + CHANGELOG.
