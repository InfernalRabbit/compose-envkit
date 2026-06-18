# cenvkit C3 — clean rename (`.cenvkit.envchain` / `CENVKIT_ENV`, NO aliases)

> Authored by the architect from the C3 plan-gate (approved 2026-06-19). **CLEAN BREAK — no back-compat aliases** (user directive; cenvkit pre-1.0). PROD = go-engineer; `*_test.go` + fixtures = qa; docs + git = architect.

**Goal:** Rename cenvkit's own selector + chain file to `CENVKIT_ENV` / `.cenvkit.envchain` as the ONLY names. Old `COMPOSE_ENV` / `.docker-env-chain` / `${COMPOSE_ENV}` stop working. Breaking, pre-1.0.

## Global Constraints
- **`COMPOSE_ENV_FILES` is a REAL docker-compose var — NEVER renamed.** Only cenvkit's own `COMPOSE_ENV` selector + `.docker-env-chain` file + the `${COMPOSE_ENV}` interpolation token migrate.
- **Seeding = A:** cenvkit injects `CENVKIT_ENV=<tier>` into the merged env (chain.go:200-201) so `${CENVKIT_ENV}` interpolates in COMPOSE_FILE + compose YAML. The monorepo's `${COMPOSE_ENV}` tokens migrate to `${CENVKIT_ENV}`.
- **Tokens:** keep `${ENV}` (generic, not a fallback), add `${CENVKIT_ENV}`, drop `${COMPOSE_ENV}`.
- **NO fallback/alias** anywhere: `.docker-env-chain` not read; `COMPOSE_ENV` not read as a selector.
- Full gate per CLAUDE.md before integration. Git is the architect's.

## PROD (go-engineer-c3 — 4 files)
- **C3-1 `internal/chain/chain.go`:** `readChainTemplates` `.docker-env-chain`→`.cenvkit.envchain` (:131 open, :136/:149 errors, no fallback); `resolveComposeEnv` read `CENVKIT_ENV` (:93 shell, :98 `.env`, :96 comment); injected var :200-201 `COMPOSE_ENV`→`CENVKIT_ENV`; `substituteTokens` :120-128 keep `${ENV}`/add `${CENVKIT_ENV}`/drop `${COMPOSE_ENV}` (:123); `defaultChain` :32 `.${CENVKIT_ENV}.env`; doc comments :1/:26/:31/:91.
- **C3-2 `internal/engine/discover.go`:** `interpolateComposeFile` :27-31 — `seedLookup` `COMPOSE_ENV`→`CENVKIT_ENV` (:28), replacer `${COMPOSE_ENV}`→`${CENVKIT_ENV}` keep `${ENV}` (:29); comments :23/:38/:82/:90-91.
- **C3-3 `cmd/cenvkit/main.go`:** selector injections `"COMPOSE_ENV="+env`→`"CENVKIT_ENV="+env` at :156 (resolvePopulator), :288, :302 (validate); flag help :498/:543; comments :119/:144. **Lines :241/:300 set `COMPOSE_ENV_FILES=` (REAL compose var) — NOT touched.**
- **C3-4 `internal/provenance/render.go` (OUTPUT-CHANGING → sequenced):** `--overview` header `COMPOSE_ENV = %s`→`CENVKIT_ENV = %s` (:219/:221); struct comment `ComposeEnv` :87. Land + freeze + report exact new header, THEN cue qa.

## QA (fixtures + `*_test.go`; go-engineer LISTS, qa edits)
- **Fixtures (examples/monorepo):** rename committed `.docker-env-chain`→`.cenvkit.envchain` (+comments :5-6); `example.env` selector `COMPOSE_ENV=dev`→`CENVKIT_ENV=dev` (:9), `COMPOSE_FILE=...${COMPOSE_ENV}.yml`→`${CENVKIT_ENV}` (:18), comments :13-15; `web/docker-compose.yml:26` `${COMPOSE_ENV:-dev}`→`${CENVKIT_ENV:-dev}`; comment-only refs in example.dev/prod.env, docker-compose*.yml, web/.web.*.env, README.md (:18,24,26,161,166). Re-seed local `.env`/`.dev.env`/`.prod.env` (gitignored) after edits. **`internal/bootstrap/bootstrap.go` = generic copier → ZERO changes.**
- **Tests:** `test/seam_test.go:78-89` (`cr.Vars` emits `CENVKIT_ENV=dev`; OSEnv :39/:102); `test/cenvkit-acceptance_test.go` (~30 `COMPOSE_ENV=` injections → `CENVKIT_ENV=`; `--value --var COMPOSE_ENV`→`CENVKIT_ENV` :594; `.env` assert :142-143; `--overview` header :1357+; chain-file writes :353/:532/:856); `internal/provenance/render_test.go:243`; `internal/chain/chain_test.go`; `internal/engine/{discover,engine,provenance}_test.go`; `cmd/cenvkit/main_test.go`.
- **Stale-count guard (kills the 4th-recurrence class):** add a self-checking guard — `const declaredAssertions = N` + a `TestAssertionCountHeader` that FAILS if it ≠ the actual asserted total (single source of truth), OR drop the absolute count entirely. qa picks the exact form.

## Breaking-change note (architect)
`CHANGELOG.md` `[Unreleased]/Changed` — ⚠ BREAKING (pre-1.0): `.docker-env-chain`→`.cenvkit.envchain` (old name no longer read); selector `COMPOSE_ENV`→`CENVKIT_ENV`; `${COMPOSE_ENV}` token→`${CENVKIT_ENV}` (`${ENV}` still works). Migration: rename the chain file + `s/COMPOSE_ENV/CENVKIT_ENV/` in `.env` + compose YAML. `COMPOSE_ENV_FILES` unaffected.

## Sequencing
go-engineer lands C3-1/-2/-3 + C3-4 (freezes + reports the exact new `--overview` header). The rename BREAKS tests until qa migrates — expected. qa then migrates fixtures + tests + the guard in one wave against the frozen prod. Architect runs the full gate on the final frozen tree → code-review → one squashed C3 commit by file + the CHANGELOG.
