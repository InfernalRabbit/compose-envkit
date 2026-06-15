---
name: glob-vs-include-acceptance-class
description: legacy smoke-monorepo discovery is find-by-glob+depth; compose-go is include-graph only — several "positive" acceptance assertions must INVERT, not just 9/10
metadata:
  type: project
---

The legacy `test/smoke-monorepo.sh` discovers subproject env_files by
`find -maxdepth $COMPOSE_DEPTH` + filename glob (`lib/compose-env.sh`), NOT by the
root compose's `include:` graph. The Go engine uses compose-go's include-graph
(`internal/engine` over `cli.LoadProject`), which ONLY reaches subprojects the
root `docker-compose.yml` actually `include:`s.

**Fixture fact (verified 2026-06-15):** `examples/monorepo/docker-compose.yml`
`include:`s exactly web/, api/, services/reports/. Scenarios that create
NON-included stray subprojects at test time and assert they ARE discovered:
- **9** (`extra/docker-compose-extra.yml`) — flagged as inversion G1. ✓
- **11** (`a/b/c/docker-compose.yml`, depth-4) — flagged G3 (drop). ✓
- **22** (`vendored/.git` gitlink + `vendored2/.git` dir) — **NOT flagged**; the
  plan + spec §13 + acceptance-port-plan §1[22] treat it as a positive E
  assertion ("includes the vendored subproject's env files"). Under include-graph
  these are NEVER discovered → assertion is unsatisfiable. SAME class as 9, missed.

**Why:** the inversion list (G1-G5) was derived per-scenario; scenario 22's glob
dependency was hidden behind a ".git pruning" framing, so it read as a positive
"discovery survives .git" test instead of "glob discovers non-included subdir".

**How to apply:** when reviewing any acceptance-port that swaps a find/glob engine
for an include-graph engine, grep the fixture's `include:` block FIRST, then for
every scenario that `mkdir`s a subproject at test time, check it is in `include:`.
If not, the assertion must invert (NOT discovered) or be dropped — assert the
glob list, not the inversion list. Related: [[spec-circular-interpolation-class]].
