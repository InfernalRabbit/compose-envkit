# TEAM.md — compose-envkit team protocol (canonical)

**NOT auto-loaded by teammates** — `CLAUDE.md` points here; teammates must read
this at task start. The heaviest rules are duplicated into `CLAUDE.md` + agent
bodies + the spawn prompt.

## Responsibility matrix (no overlap)

| Agent | Owns | Off-limits |
|---|---|---|
| architect (lead) | `.claude/`, `docs/`, planning, ALL git surgery, synthesis | editing code (read-only) |
| go-engineer | `cmd/cenvkit/`, `internal/**`, `go.mod`, `go.sum` | `*_test.go`, `test/`, docs |
| qa-engineer | `**/*_test.go`, `test/` (Go + ported smoke acceptance) | prod code (report to go-engineer) |
| code-reviewer | `.claude/artifacts/` (report only) | editing anything (read-only) |

## Model policy (approved by user 2026-06-15)

| Role | Model | Approved |
|---|---|---|
| architect | Opus 4.8 | user 2026-06-15 |
| go-engineer | Opus 4.8 | user 2026-06-15 |
| qa-engineer | Sonnet 4.6 | user 2026-06-15 |
| code-reviewer | Opus 4.8 | user 2026-06-15 |
| default (no `model:`) | Sonnet 4.6 | user 2026-06-15 |

Priority: **balance**. Per-task override: lead may **bump** a role to Opus
autonomously (logs it) and **downgrade** for mechanical tasks autonomously (logs
it). A NEW role or a standing model change → re-ask the user (new
`AskUserQuestion`). Next config update: ask the user only about the **delta** vs
this table.

## Routing & effort

- Trivial fix/question → main session, no agents.
- One focused return task (research / verify / point-fix) → 1 subagent.
- ≥2 independent or collaborative branches → real Agent Team (shared task-list +
  mailbox), created at the START.
- Codebase-scale (hundreds of like edits) → propose Dynamic Workflows (cost ↑↑;
  requires v2.1.154+; confirm model-for-mass-agents with the user first).
- Honest note: the v1 Go core is partly sequential (engine interface → tests →
  review). The team's parallel wins are qa porting acceptance + reviewer + docs
  alongside the engineer. Don't spin teammates for sequential single-file work.

## Handoff = task-list entries

Each unit of work is a `TaskCreate` with subject + (goal / files / stack / what's
done / what to verify / boundaries) in the description, `owner` set AT creation,
and `blockedBy` for sequential deps (e.g. the engine-interface task blocks the
qa tests-against-engine task). Teammates inherit neither the lead's chat nor this
file — context goes in the task.

## Team communication

The lead is addressed as **`team-lead`** in `SendMessage` (not "architect"); peers
by their teammate name (`go-engineer`/`qa-engineer`/`code-reviewer` or the spawn
name). Tasks/handoffs that say "report to the lead" mean DM `team-lead`.
Teammates already carry **context7** + **WebSearch/WebFetch** in their `tools:`
(probe-confirmed) — use them for API/doc/research lookups instead of guessing.

`SendMessage` to the lead for status; claim/close your task in the shared
task-list; **DM a peer by name** for cross-cutting (engine interface, fixtures,
shared env) — not via the lead. After your task, immediately claim the next
unblocked task (don't park in idle); don't send duplicate "already done" recaps —
the source of truth is `TaskUpdate` + one report. The lead batches idle
notifications and sends ONE source-of-truth message when clearing confusion.

## Plan-approval (risky work)

Security/migrations/the compose-go engine contract/hard-to-reverse → lead spawns
a **fresh** teammate in plan-mode (`mode:"plan"`): read-only plan → lead approval
→ implementation. Never a manual "send plan, wait OK" gate on a reused idle agent
(mailbox crossover).

## Planning & cross-module assumptions

Depend on a detail inside another module → ask the owner (mailbox Q&A) or do a
read-only detail-check; don't guess. Any claim about a helper/API/contract in a
plan cites a `file:line` you actually opened — the lead bounces unsourced claims
at the plan gate. **This includes quotes:** quoting an existing file → open it and
quote literally (a paraphrase that fails grep burns a verify cycle). Find
cross-layer bugs by a mechanical diff of normalized contract-surface maps across
layers; keep those maps as living artifacts.

## Decision consult (principle 15)

Before a non-trivial decision (contract/schema, secrets, UX semantics,
cross-lane interface, hard-to-reverse) send a compact consult to the architect
and/or peer owner: context `file:line` · options considered · your lean · ONE
question. The lane owner decides (consult is input, not a vote); log it in the
task artifact. Anti-stall: no answer by decision time → reversible: decide by
lean with a note; irreversible/secrets: escalate to lead. Trivial in-lane choices:
no consult (don't spam the mailbox).

## Session-boundary & phantoms (principle 16)

Team-state (team + shared task-list) does NOT survive a session boundary — it
vanishes silently. Lead on a new session: `TeamCreate` FIRST → create tasks →
`owner` at creation → clean orphan task-lists (`status=deleted`). Teammate on a
stale/phantom (re-)assignment of done work: **verify-and-dismiss** — truth is on
disk (commit hash/files/artifact); don't re-do, don't amend, don't ping per
phantom (one batch note if they arrive in a flurry).

## Git discipline (single repo, branch master)

One squashed commit per milestone. Teammate: squash → report `hash` + `git diff
--stat` → DO NOT touch git until lead verifies + pushes; zero git on hold/idle.
**Lead owns all git surgery**: verify on disk, group commits by whole files
(`git add <files>`, never `add -A`; `git add -p` unavailable), push. Before
expensive verification against a reported hash, ensure the teammate is frozen on
it; re-check `HEAD == verified hash` before pushing. (No submodules here — plain
git.)

**Committed-tree rule (2026-06-16):** when staging only a subset of the working
tree during multi-agent flux, verify the COMMITTED state, not the working tree.
After `git add <subset>`: `git stash -u && go test ./... -count=1 && git stash
pop`. A green working-tree test while other agents are editing can hide a broken
staged subset. Freeze ALL teammates before running a "final" verify — a raced
verify yields stale, misleading results.

## blockedBy / completed discipline

Don't start or commit a `blockedBy` task without asking the lead. `completed` only
with FULL DoD — a half-done task stays `in_progress` with a note ("will close
after X" is NOT completed).

## Integration sequencing & report fidelity (lessons 2026-06-17)

- **Output-changing prod edits → SEQUENCE, don't parallelize.** When a prod change
  alters human/CLI output that tests assert (e.g. a renderer tweak), have the impl
  agent land + freeze + report the EXACT new output FIRST, then cue qa to match
  tests to it. Running both in parallel makes existing tests red mid-flight (a
  render-vs-test race that bit us). Pure-additive prod (new symbols, no output
  change) may run parallel with test authoring.
- **Reports must reflect the SHIPPED disk state, not prior messages.** A completion
  report that repeats a stale comment/number contradicting the actual code burns a
  verify cycle. Pin claims to what's on disk now.
- **Lead verifies on DISK, never on the report.** Every "green/done" is re-checked
  by the lead with the full gate (CLAUDE.md "Full gate") on the frozen tree before
  commit — this caught a docker-gated miss + gofmt twice + a stale count this round.

## Definition of Done — bands

- **Band A (new feature / code change):** `gofmt` clean + `go vet` + `go build` +
  `go test` green; **mandatory code-reviewer review**; lessons captured; then
  integration.
- **Band B (accepting existing WIP):** verification green + **mandatory
  secrets-scan**; review optional at lead's discretion.

## Auto-learning (Capture → Reuse → Promote)

Read your memory at task start (Reuse); write a lesson when review bounces your
work / the user corrects you / you hit a non-obvious gotcha (Capture) BEFORE
"done"; reviewer→owner→fix→lesson loop. Lead periodically promotes recurring
lessons into `CLAUDE.md` / agent bodies.

## Memory

Single repo → `memory: project` works normally (no monorepo absolute-path
caveat). Lead consolidates cross-role lessons.

## Blocked & destructive actions (anti-stall)

Require confirmation: `git push`, `rm -rf`, anything destructive, any MCP with
external side effects (none configured here; serena edits are local). On a
blocked step: escalate to the lead with the exact command and continue unblocked
work — never spin.

## Conflict resolution

The architect synthesizes and resolves conflicts.
