# CLAUDE.md — compose-envkit dev team

Auto-loaded by every teammate. `.claude/TEAM.md` is the FULL protocol but is
**not** auto-loaded — **teammates: read `.claude/TEAM.md` at the start of your task.**

## Project overview

**`cenvkit`** is a Go CLI (on Docker's own `compose-go` loader; installable +
vendorable) that does two coherent things:

1. **Gap-debugger (the moat).** It detects + explains the Docker Compose
   `env_file:`→`${VAR}` interpolation gap — a `${VAR}` satisfied only by a service
   `env_file:` silently falls back at the run (docker/compose#3435, never fixed
   upstream; this is the uncontested niche). `cenvkit env-debug` surfaces it with
   full provenance, daemon-free; **`cenvkit gap-report`** is a CI/pre-build lint
   (exit 1 = gaps / 0 = clean / 2 = no compose file).
2. **Env-chain populator (the local arm).** It delivers the same Layer-1 chain to
   whatever consumes it: `cenvkit compose` (→ `COMPOSE_ENV_FILES`, exec
   `docker compose`), **`cenvkit run -- <cmd>`** (exec any process with the merged
   env, no docker), **`cenvkit env`** (emit dotenv/json/shell). One tool for compose
   AND local dev — it *composes with* make/just, it does not replace them.

Chain file **`.cenvkit.envchain`**; selector **`CENVKIT_ENV`** (token
`${CENVKIT_ENV}`, alias `${ENV}`); optional `[name]` sections selected by
`--chain <name>`. compose-go is isolated behind `internal/engine`; the one
expansion path keeps `cenvkit env --expand` == `env-debug --effective` ==
`docker compose config`. The POSIX-`sh` kit was removed — cenvkit is the only
implementation. Design:
`docs/superpowers/specs/2026-06-19-cenvkit-populator-and-gap-debugger-design.md`
(+ the 2026-06-15/-17 specs for history).

## Module boundaries (no overlap; refuse work outside your zone)

| Owner | Owns | Never touches |
|---|---|---|
| **architect** (lead) | `.claude/`, `docs/`, planning, ALL git surgery, synthesis | edits code (read-only) |
| **go-engineer** | `cmd/cenvkit/`, `internal/**`, `go.mod`, `go.sum` | `*_test.go`, docs |
| **qa-engineer** | `**/*_test.go`, `test/` (Go + ported smoke acceptance) | prod code (report bugs to go-engineer) |
| **code-reviewer** | `.claude/artifacts/` (report only) | edits anything else (read-only) |

## Verification commands

- Go: `go build ./...` · `go vet ./...` · `gofmt -l .` (must be empty) ·
  `go test ./... -count=1` (and `golangci-lint run` if installed).
- Acceptance: `go test ./test/...` — docker-gated, drives the `cenvkit` binary
  against `examples/monorepo` (the ported smoke suite); also run with
  `SMOKE_SKIP_DOCKER=1` for the no-docker subset.
- **Full gate before declaring green / integrating:** `gofmt -l .` empty AND
  `go test ./... -count=1` AND the docker acceptance path (`go test ./test/...`
  with docker up). NEVER report green on `SMOKE_SKIP_DOCKER=1` alone, nor on
  `go test` without `gofmt` — both have shipped real misses (a docker-gated
  assertion unrun under a behavior change; gofmt, twice).

## Conventions

- **Upstream-first:** `compose-go` is the source of truth for compose semantics —
  do not reimplement or diverge; pin its version, bump deliberately + re-run
  acceptance.
- Go: `gofmt`, wrapped errors with context, table-driven tests, small focused
  packages (`internal/chain`, `internal/engine`, `internal/provenance`).
- Safety rules: **no `sudo`, no `chmod 777`, no secrets written to disk**. Secret
  *management* is **out of scope** (no masking/encryption/backends); `.secrets.env`
  just loads **last** in the chain (last-wins). External managers wrap cenvkit
  (`sops exec-env -- cenvkit run …`).
- POSIX `sh` for any shipped shell (the vendor shim); `sh -n` clean.

## Operating principles

- **Effort rule:** trivial fix/question → main session, NO agents. One focused
  return task (research/verify/point-fix) → **1 subagent**. ≥2 independent/
  collaborative branches → **real Agent Team** (shared task-list + mailbox).
  Codebase-scale (hundreds of like edits) → propose **Dynamic Workflows** (cost ↑↑).
- **Handoff format** (every task): goal · result format · tools/files · boundaries
  (what NOT to touch) · stop criteria. Teammates inherit NEITHER the lead's chat
  NOR `TEAM.md` — put all context + protocol in the task/spawn prompt.
- Teammates: **read `.claude/TEAM.md` at task start** (not auto-loaded).

## Artifacts protocol

Write code to the repo; write reports/plans/reviews to `.claude/artifacts/` on
disk. To lead/peers send a **summary + file link**, not a wall of text.

## MCP access (least privilege)

- **How teammates get a plugin MCP (verified 2026-06-15):** list its tool names
  in the agent's **`tools:` field** — that's the gate. context7 + serena are
  plugin-provided (no project `.mcp.json` needed); a teammate whose `tools:`
  omits them can't call them (that was the dry-run failure).
- **context7** (`mcp__plugin_context7_context7__resolve-library-id` / `__query-docs`)
  — current docs for `compose-go`, cobra, Go stdlib. In every teammate's `tools:`;
  probe-confirmed reachable. Lead has it via full toolset.
- **serena** — semantic Go nav/edit (LSP), plugin-provided. NOT in teammate
  `tools:` by default (heavier startup). Add `mcp__plugin_serena_serena__*` to an
  agent's `tools:` to grant it; else use the `go doc`/`go get` verified-probe
  fallback. Lead has it.
- **WebSearch / WebFetch** — in teammate `tools:` for research; probe-confirmed.
- No MCP with external side effects exists here; serena edits are local file
  edits. Anything genuinely destructive requires explicit confirmation.
- **MCP-unreachable fallback (dry-run lesson):** a teammate session may not have
  context7/serena reachable. If so, fall back to a **verified primary-source
  probe** (e.g. `go doc` against the real module) and note the fallback in your
  report — never guess an API. The lead is addressed as **`team-lead`** in
  `SendMessage`.

## Hard constraints

- **Do NOT touch `/Users/infernal_rabbit/Workflow/Big/Access/monorepo`** — separate
  repo, off-limits without explicit user go-ahead.
- Secrets never committed. One squashed commit per milestone; **git surgery is
  the lead's job** (see `.claude/TEAM.md`).
