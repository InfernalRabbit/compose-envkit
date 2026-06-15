# CLAUDE.md — compose-envkit dev team

Auto-loaded by every teammate. `.claude/TEAM.md` is the FULL protocol but is
**not** auto-loaded — **teammates: read `.claude/TEAM.md` at the start of your task.**

## Project overview

`compose-envkit` closes the gap where a service `env_file:` is invisible to
Docker Compose's compile-time `${VAR}` interpolation. The POSIX-`sh` kit
(`lib/`, `mk/`, `bin/docker`, `templates/`) is **frozen legacy/reference**. Active
work is the **`cenvkit` Go rewrite**: a Go CLI on `github.com/compose-spec/compose-go`
(Docker's own loader), distributed dual-mode (installable + vendorable), v1
"thin" (assemble `COMPOSE_ENV_FILES`, exec `docker compose`). Spec:
`docs/superpowers/specs/2026-06-15-cenvkit-go-rewrite-design.md`.

## Module boundaries (no overlap; refuse work outside your zone)

| Owner | Owns | Never touches |
|---|---|---|
| **architect** (lead) | `.claude/`, `docs/`, planning, ALL git surgery, synthesis | edits code (read-only) |
| **go-engineer** | `cmd/cenvkit/`, `internal/**`, `go.mod`, `go.sum` | `*_test.go`, docs, legacy sh kit |
| **qa-engineer** | `**/*_test.go`, `test/` (Go + ported smoke acceptance) | prod code (report bugs to go-engineer) |
| **code-reviewer** | `.claude/artifacts/` (report only) | edits anything else (read-only) |

**Legacy sh kit** (`lib/ mk/ bin/docker templates/ install.sh test/smoke*.sh test/lint.sh`):
do NOT modify except on explicit lead direction — it is the parity reference.

## Verification commands

- Go (target): `go build ./...` · `go test ./...` · `go vet ./...` · `gofmt -l .`
  (and `golangci-lint run` if installed).
- Acceptance / legacy (still valid): `sh test/lint.sh` · `sh test/smoke.sh` ·
  `sh test/smoke-monorepo.sh` (61 assertions; the Go tool must keep these green
  once ported to drive `cenvkit`).

## Conventions

- **Upstream-first:** `compose-go` is the source of truth for compose semantics —
  do not reimplement or diverge; pin its version, bump deliberately + re-run
  acceptance.
- Go: `gofmt`, wrapped errors with context, table-driven tests, small focused
  packages (`internal/chain`, `internal/engine`, `internal/debug`).
- Carried safety rules (from the sh kit): **no `sudo`, no `chmod 777`, no secrets
  written to disk**; secrets load **last** in the chain (last-wins).
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
- Do NOT rewrite the legacy sh kit except on lead direction (it is the reference).
- Secrets never committed. One squashed commit per milestone; **git surgery is
  the lead's job** (see `.claude/TEAM.md`).
