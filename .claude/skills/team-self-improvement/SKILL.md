---
name: team-self-improvement
description: Use when asked for a self-improvement pass, team retro, "самообучение", or to improve/refine the agent team's config (CLAUDE.md, .claude/TEAM.md, .claude/agents/*.md, agent memory) after a stretch of work.
---

# Team Self-Improvement Pass

## Overview

Refine the agent team's configuration from OBSERVED failure modes — grounded in
evidence, **proposed for approval, never applied silently.** Checked-in config
(CLAUDE.md, TEAM.md, agent bodies) and model assignments are the user's; you
propose, they decide.

**Core rule: violating the letter (silent edit) is violating the spirit. Propose
first.**

## The Iron Rule

**NEVER silently edit shared config or change a model.** Every change to
`CLAUDE.md` / `.claude/TEAM.md` / `.claude/agents/*.md` / model assignments goes
through an `AskUserQuestion` first. Model changes (new role, standing change)
ALWAYS require explicit user approval — no exceptions, no "it's obviously better."

Your OWN architect memory (`.claude/agent-memory/architect/`) you may write
directly — it's yours.

## Process

1. **Gather evidence** — `git log --oneline`, `.claude/artifacts/`, agent memory,
   and the actual session. Every proposal cites a concrete incident
   (`file:line`, commit hash, or "recurred N×"). No evidence → not a proposal.
2. **Find recurring failure modes** — duplicated work, wrong boundaries, narrow
   verify gates, stale config, report↔code drift, model mis-fit. Prefer patterns
   that bit ≥2×.
3. **Propose via `AskUserQuestion`** (multiSelect) — one option per refinement,
   each naming the exact file + the evidence. Model policy: only propose a model
   change as its own question; otherwise state "policy held, no change."
4. **Apply only approved** — edit the named files. **Consolidate**: one source of
   truth per rule; promote a recurring lesson into the config teammates READ
   (CLAUDE.md / agent body), not five copies. Don't duplicate an existing rule.
5. **Capture durable lessons** in agent memory (link related `[[notes]]`).
6. **Verify + commit** — full gate (see house lessons), then propose the commit.

## House lessons to enforce (this team)

- **Full verify gate:** `gofmt -l .` empty AND `go test ./... -count=1` AND the
  docker acceptance path (`go test ./test/...`). NEVER green on
  `SMOKE_SKIP_DOCKER=1` alone, nor `go test` without `gofmt`.
- **Single source of truth** for counts/numbers (e.g. assertion count in ONE
  header) — no fragile inline running tallies.
- **Sequence output-changing prod before the tests** that assert it (impl freezes
  + reports exact output → qa matches). Don't parallelize that.
- **Lead verifies on disk, not on reports.** Reports/comments reflect shipped
  state, not prior messages. Verify-before-claim (cite `file:line`).

## Red Flags — STOP

- Editing CLAUDE.md / TEAM.md / an agent body / a model WITHOUT an
  `AskUserQuestion` → stop, propose.
- A proposal with no `file:line` / commit / "recurred N×" evidence → it's an
  opinion, not a finding.
- Adding a rule that already exists elsewhere → consolidate, don't duplicate.
- "It's a small/obvious improvement, I'll just edit it" → shared config is the
  user's; propose.

| Rationalization | Reality |
|---|---|
| "Obviously better, I'll just edit" | Checked-in config is the user's. Propose via question. |
| "I'll bump the model, it fits" | Model changes ALWAYS need user approval. Never silent. |
| "More memory notes = improvement" | Recurring lessons go into config teammates READ; memory is the lead's own context. |
| "I read the configs, that's the evidence" | Evidence = incidents (file:line/commit/N×), not a read-through. |
