---
name: cenvkit-c1-c4-build-orchestration
description: 2026-06-19 — the populator+gap-debugger build (C1-C4) shipped + merged to master; the per-cycle plan-gate→parallel-build→committed-tree-gate cadence that worked + the 5 broken-HEAD issues the disk-gate caught
metadata:
  type: project
---

The C1–C4 reshape (C1 `gap-report` · C2 `run`/`env` populator · C3 clean rename
`.cenvkit.envchain`/`CENVKIT_ENV` · C4 named chains) shipped and fast-forward-merged
to **master** on 2026-06-19 (commits `3652a3a`→`abc0127`; the user chose
merge-without-PR). Spec:
`docs/superpowers/specs/2026-06-19-cenvkit-populator-and-gap-debugger-design.md`;
per-cycle plans in `docs/superpowers/plans/2026-06-19-cenvkit-c{1,2,3,4}-*.md`.

**Orchestration cadence that WORKED — reuse it for multi-cycle feature work:**
- **One cycle = one fresh plan-mode go-engineer authors the impl plan READ-ONLY**
  for engine-contract / non-trivial cycles (C2 one-engine unification, C3 rename,
  C4 named chains); the architect pre-authors the plan for a simple ADDITIVE cycle
  (C1). Approve at the plan-gate (cite `file:line`; bounce unsourced claims). On
  approval, tell a plan-mode teammate to **"exit plan mode and implement"** (plain
  "approved" left go-engineer-c2 idle).
- **Build: go-engineer (prod) ∥ qa (tests) in PARALLEL on disjoint files.**
  Output-changing prod (e.g. the env-debug `--chain`→`--list` flag rename, a
  renderer tweak) is SEQUENCED: impl lands + freezes + reports the EXACT new output
  FIRST, then cue qa — never parallel (render-vs-test race).
- **code-reviewer APPROVE** (it temp-breaks remediation guards to prove
  RED-on-drift).
- **Architect committed-tree gate on the FROZEN tree** (all teammates idle):
  `gofmt -l .` empty + `go test ./...` + the docker acceptance path. Then an atomic
  squash: a `feat(...)` commit (prod + tests + plan) + a `chore(team)` records
  commit (memory + review artifact).

**The disk-gate ("verify on disk, not on reports") caught 5 broken-HEAD-class issues
pre-commit** — the strongest validation of the discipline: a docker-gated assertion
masked by a `&& echo`; a `gofmt` miss on a late S1 edit; a blind `s/COMPOSE_ENV/`
over-reaching the REAL `COMPOSE_ENV_FILES`→`CENVKIT_ENV_FILES`; a gitignored `.ci.env`
fixture that wouldn't commit; the missing `example.*` seed for it.

Self-improvement 2026-06-19 folded 4 of these into config (rename→grep-docs in
`code-reviewer.md`; gofmt-after-final-edit in `CLAUDE.md`; fixtures-tracked-`example.*`-seed
in `qa-engineer.md`; plan-mode-exit in `.claude/TEAM.md`). **Model policy HELD** —
`qa=Sonnet` handled all test/fixture work cleanly; the Opus roles
(architect/go-engineer/code-reviewer) carried the hard design (U1 unification, the
gap-debugger) + review. See also [[competitive-landscape-positioning]].
