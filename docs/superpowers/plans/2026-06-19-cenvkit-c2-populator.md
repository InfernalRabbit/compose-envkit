# cenvkit C2 — generic populator (`run` / `env`) Implementation Plan

> **For agentic workers:** implement task-by-task; PROD-only for go-engineer, `*_test.go` for qa, docs+git for the architect. Authored by the architect from the plan-gate go-engineer's code-grounded investigation (approved 2026-06-19).

**Goal:** Add `cenvkit run -- <cmd>` (populate the Layer-1 chain → exec a process) and `cenvkit env` (emit the merged env), the no-docker "local arm" — same chain, same values as compose.

**Architecture:** A new compose-go-backed `engine.Flatten` (the only place that expands `${VAR}` via `dotenv.GetEnvFromFile`) feeds BOTH a new pure-Go `internal/envmap` (shell-wins overlay + emit/quoting) AND env-debug's interpolation env (unification U1). `cmd` gains `run`/`env`, reusing C1's `exitError` seam. Compose-go stays isolated behind `internal/engine`.

**Tech Stack:** Go, cobra (`ArgsLenAtDash`), compose-go v2.11.0 `dotenv` (behind engine), stdlib.

## Global Constraints

- **Seam (hard):** compose-go ONLY in `internal/engine`. `internal/envmap` is pure-Go (imports `internal/engine` for `Flatten`/`ParseOrderedLiteral`, never compose-go directly).
- **Single expansion path (§5c MUST-FIX):** `cenvkit env --expand` == `env-debug --effective` == `docker compose config`. Both the populator and `provenance.go`'s interpEnv route through `engine.Flatten` (U1).
- **shell-wins precedence:** process env overlays chain values (add-only-unset, mirrors `cli.WithDotEnv` `Mapping.Merge`). `--no-expand` suppresses `${VAR}` only; shell-wins still applies. Unset `${VAR}` w/o default → empty (compose parity).
- **Exit codes (run):** reuse `exitError`; child `*exec.ExitError`→its code, missing binary→127, non-exec→126, signal→128+signo. `--` REQUIRED (empty post-`--` → 2).
- **Determinism:** `env`/`--print` output key-sorted (chain.Resolve already sorts at chain.go:207). JSON never styled.
- **Verification gate per CLAUDE.md** before integration (`gofmt -l .` empty, `go vet`, `go test ./...` with docker up). **Git is the architect's.**

## Decisions (approved 2026-06-19; from the plan-gate investigation)

- **D1 — Seam:** `engine.Flatten` (compose-go) in `internal/engine`; `internal/envmap` pure-Go (overlay + `--no-expand` via `engine.ParseOrderedLiteral` + emit/quoting); `--expand` calls `engine.Flatten`.
- **D2 — Unification = U1:** route the populator AND `provenance.go`'s Layer-1 interpEnv through one `engine.Flatten(base, files, expand=true)` wrapping `dotenv.GetEnvFromFile`. **Probed: zero acceptance-golden drift on the monorepo** (the only in-value ref is `COMPOSE_FILE=…${COMPOSE_ENV}…`, `COMPOSE_ENV` set earlier in the same file → identical both ways). Preserve the interpEnv-vs-chainEnv split; chainEnv stays incremental.
- **D3 — shell-wins:** `final := clone(processEnv); for k,v := range chainVals { if _,set:=final[k]; !set { final[k]=v } }`. `env` emits chain-derived keys only (sorted); `run` execs the full merged env.
- **D4 — run parser/exec:** cobra `ArgsLenAtDash()` (NOT `DisableFlagParsing` — `run` needs its own flags): `<0`→`-- required` exit 2; `>=0 && len(args)==0`→empty exit 2. `exec.Command` + signal forwarding (not `syscall.Exec`; keeps `--print` simple). Exit mapping via `exitError` + `ProcessState`.
- **D5 — `env --format` quoting (eval-safety):** `shell` single-quote + `'\''` idiom, reject non-identifier keys; `dotenv` quote/escape round-tripping compose-go's parser; `json` stdlib. `--no-expand` → `engine.ParseOrderedLiteral` (no 3rd literal reader).

## Tasks (PROD; go-engineer)

- **T1 — engine: export literal reader + add `Flatten`.** Rename `parseOrderedLiteral`→exported `ParseOrderedLiteral` (`internal/engine/provenance.go:54`; update callers at :207, :319; **grep `*_test.go` for refs → report to qa**). New `internal/engine/flatten.go`: `func Flatten(base map[string]string, files []string, expand bool) (map[string]string, error)` — `expand`→`dotenv.GetEnvFromFile(base, files)`; `!expand`→per-file `ParseOrderedLiteral` last-wins. Returns file-vals-only; errors on a missing file (callers pass existence-filtered paths only).
- **T2 — engine: unify interpEnv onto `Flatten` (U1).** In `Provenance` (`provenance.go:158-184`) build the Layer-1 interpEnv via `engine.Flatten(shellMap, layer1Paths, true)`; keep chainEnv incremental. **After T2: full acceptance incl. gap-report + env-debug + 66 docker scenarios = zero drift (confirm before proceeding).**
- **T3 — `internal/envmap` (NEW, pure-Go).** `Resolve(cr chain.Result, processEnv map[string]string, expand bool) (map[string]string, error)` (Flatten + shell-wins overlay); `Emit(m, format)` (D5 quoting); `ChainKeys(m)` sorted. No compose-go import.
- **T4 — cmd: `cenvkit env`.** `newEnvCmd()`: `-e/--env`, `--expand|--no-expand` (default expand), `--format dotenv|json|shell`. `-e` → `COMPOSE_ENV=<v>` overlay (like `validate --all`, main.go:241). Empty chain → no lines, exit 0.
- **T5 — cmd: `cenvkit run`.** `newRunCmd()`: chain flags + `--print`; `ArgsLenAtDash` enforcement; `--print`→emit dotenv + exit 0 (skip exec); else `exec.Command` full-merged-env + signal forwarding + exit mapping via `exitError`. Register both in `newRootCmd()` (main.go:67).

## Test plan (qa; authored AFTER go-engineer freezes prod — output-changing T2)

- **envmap unit:** last-wins; shell-wins on both `--expand`/`--no-expand`; `--no-expand`==`ParseOrderedLiteral`; never-handed-missing-path; unset `${VAR}`→empty; each format round-trips space/newline/quote/`$`.
- **run unit:** `--` enforcement (exit 2 both cases); exit 127/126/128+signo; `--print` exit 0.
- **Parity acceptance (docker-gated):** `env --expand` == `env-debug --effective` == `docker compose config` for plain var / `${VAR:-def}` set+unset / shell override / special-char / the gap var.
- **Rename fix:** update any `*_test.go` reference to `parseOrderedLiteral` (list comes from T1).

## Integration sequencing

Output-changing T1+T2 (rename + engine unification) land + freeze FIRST; go-engineer confirms zero golden drift; THEN qa authors C2 tests (no render-vs-test race). Pure-additive T3–T5 are fine. Architect runs the full gate on the frozen tree → code-review → one squashed C2 commit by file.
