# C1 `gap-report` — code review

**Verdict: APPROVE (one minor doc-nit, non-blocking).** Working tree, pre-integration.

Scope reviewed (working tree, uncommitted):
- PROD: `internal/provenance/gapreport.go` (NEW), `cmd/cenvkit/main.go` (MOD).
- TESTS: `internal/provenance/gapreport_test.go` (NEW), `cmd/cenvkit/main_test.go` (MOD),
  `test/cenvkit-acceptance_test.go` (MOD).

Contract refs: spec §6; plan `2026-06-19-cenvkit-c1-gap-report.md`.

Full gate run on this tree: `gofmt -l .` empty · `go vet` clean · `go build ./...` ok ·
provenance + cmd gap-report unit tests PASS · `SMOKE_SKIP_DOCKER=1 go test ./test/
-run TestGapReport_ExitCodeContract` PASS.

---

## Critical
None.

## Warnings
None.

## Suggestions

### S1 (minor, doc-nit) — stale assertion-count comment — `test/cenvkit-acceptance_test.go:41`
The header count was correctly bumped 111 → 115 (lines 2 and 38, `+4 C1: gap-report
exit-code contract`). But line 41 still reads:

> `// throwaway fixtures and guard contract seams — they are included in the 111 count.`

`111` is now stale; should be `115` to match the new total. Comment-only, no
behavior impact. (Owner: qa-engineer — `test/` zone.) This is a recurring class on
this suite; grep the WHOLE file for stray counts on every count-touching change
(`0o111`/`0o644` octal literals are false positives).

---

## Checklist verification (all PASS)

1. **Exit-code contract (spec §6):** `exitError{code,msg}` + `ExitCode()` defined
   `main.go:76-82`; `main()` does `errors.As(err,&ee)` → empty-msg path prints NO
   stderr line and `os.Exit(ee.code)` (`main.go` main()). Root has `SilenceErrors:
   true`+`SilenceUsage: true` (`main.go:42-43`) so cobra neither double-prints nor
   adds usage spam, and `Execute()` returns the `exitError` unswallowed. Exit 2 is
   distinct: gated on `!engine.HasComposeFileEnv(dir, cr.Vars)` BEFORE Provenance,
   returns `code:2` with a real message; gaps → `code:1` empty-msg (report already
   on stdout); clean → nil. Asserted at unit (4 sub-tests) + acceptance (4 sites),
   all green.
2. **Seam (provenance stays compose-go-free):** `go list -f '{{.Imports}}'
   ./internal/provenance/` = `[encoding/json fmt io path/filepath sort strings]` —
   stdlib only, NO compose-go, NO internal/engine. `gapreport.go` import block is
   `encoding/json fmt io sort`. compose-go isolation invariant intact (only
   `internal/engine` directly imports compose-go; verified via direct-imports grep,
   NOT `-deps`).
3. **JSON stability + no-styling:** `RenderGapReportJSON(w, gr)` takes NO Styler —
   structurally ANSI-free; field tags exactly `var/service/field/fallback/fix` +
   `count` (spec §6.5). `TestGapReportJSON_NoANSI` pins it under `--color=always`.
4. **Determinism:** `CollectGaps` sorts var names (`sort.Strings`), then emits per
   engine-sorted Effects (engine sorts service→field at provenance.go:409-414).
   `TestCollectGaps_SortedByVar` guards the var sort.
5. **Gap correctness:** consumes the EXISTING `VarTrace.Gap` set
   (`provenance.go:424` `referenced && !InChain && len(RuntimeDefs)>0`) — NO new/
   divergent logic. `Fallback` = `stripVarPrefix(name, e.Resolved)` (render.go:372),
   matching the human-render normalization. `DB_HOST=` → `""` test pins the strip.
6. **Scope:** diff grep for `cenvkit.envchain|CENVKIT_ENV|named.?chain|--chain|
   newRunCmd|newEnvCmd|envmap|mask|redact` → nothing. No C2/C3/C4 leak; no
   secret-masking.
7. **Tests pin the contract:** unit `clearGapEnv` unsets `COMPOSE_FILE/
   COMPOSE_ENV_FILES/COMPOSE_ENV/WEB_PORT` with `t.Cleanup` restore (no `t.Parallel`
   anywhere in cmd pkg → unset is race-free); acceptance `envWithout(...)` builds a
   per-child `c.Env` (fully hermetic, parallel-safe); both daemon-free. Exit codes
   asserted via `errors.As(&exitError)` (unit) and `*exec.ExitError` code (accept).
   Guard validity: these are NEW assertions over NEW behavior — a wrong exit code
   fails them by construction.
