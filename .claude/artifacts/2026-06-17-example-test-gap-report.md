# examples/monorepo — test-coverage gap report (workflow gap-analysis)

Produced by a 6-dimension parallel gap-analysis workflow (read-only) + adversarial
synthesis, 2026-06-17. Verified against real code (cmd/main.go, internal/provenance,
the staged fixture, test/). 13 genuine gaps; 2 mis-specified map items rejected.

## Already well-covered (do NOT re-add)
Chain ordering/dedup/secrets-last/tokens; COMPOSE_ENV precedence + ComposeEnvSource;
W1 sanitization; v3 run-path-L1-only inversion; gap-detector pos+both negatives;
port fallback (WEB/API/REPORTS); per-service tiers; dev/prod overlay+STACK_TIER+IS_DEV;
include-graph discovery / no-over-discovery; determinism+abs paths; full --color matrix;
--overview sections/markers/chain-override; JSON schemas; init no-clobber/fan-out;
C1/D1; --project-dir/version; chain-only project. (See report for the exact test fns.)

## Gaps to implement (architect-selected)

### HIGH
- **A. `validate` (current env)** — docker-gated. Positive: stage monorepo, COMPOSE_ENV=dev,
  `cenvkit validate` exits 0 + stdout has "config valid". Negative: scratch w/ invalid root
  docker-compose.yml → exits non-zero, stderr non-empty.
- **A2. `validate --all`** — docker-gated. `cenvkit validate --all` exits 0 + stdout has
  BOTH "dev config valid" AND "prod config valid".
- **B. `env-debug --files` two-group on the real monorepo** — non-docker. Output has
  "interpolation (COMPOSE_ENV_FILES):" + "runtime-only"; .env under interpolation; "web:"
  heading; .web.env AND .web.dev.env indented under web; .web.env NOT under interpolation.
- **E. compose-go load failure fatal** — non-docker, scratch invalid docker-compose.yml →
  `env-debug --files` exits non-zero, stderr carries the error.

### MED
- **B2. `--files --json` / `--chain --json` Layer-1-only schema** — non-docker. .files &
  .chain_files have a `.env` path but NO `.web.env`/`.api.env`/`.reports.env`; .services[web]
  .env_files contains a `.web.env` path.
- **C1. `--value --var WEB_PORT` = fallback** — non-docker. trimmed stdout == "0" (NOT "18080").
- **C2. `--trace --var REPORTS_PORT` deep gap** — non-docker. gap annotation + a runtime line
  whose abs path ends `services/reports/.reports.env`.
- **D1. default chain fallback** — scratch (no .docker-env-chain) → env-files lists .env,
  .dev.env, .secrets.env in order.
- **D2. named-missing chain file skipped** — scratch (.docker-env-chain=.env,.missing.env,
  .secrets.env; only .env+.secrets.env exist) → env-files has .env+.secrets.env, NOT .missing.env.
- **D3. `${COMPOSE_ENV}` root-chain alias** — scratch (.docker-env-chain=.env,.${COMPOSE_ENV}.env;
  COMPOSE_ENV=test, .test.env exists) → env-files includes the abs path ending .test.env.
- **D4. quoted/comment/blank chain parsing (acceptance level)** — scratch (.env w/ Q1="hello world",
  Q2='x y', a # comment, a blank) → `--value --var Q1`="hello world", `--value --var Q2`="x y".

## Test-harness fix REQUIRED for A/E (adversarial note 2)
`runCenvkit` uses `CombinedOutput()` → cannot distinguish stdout from stderr. The
validate-negative + error-path tests need a NEW helper returning stdout & stderr
SEPARATELY (e.g. `runCenvkitSplit`). Add it; use it for A-negative and E.

## DEFERRED (not this batch)
- **Fully-inline-overridden env_file in --files end-to-end** (N-3): the render LOGIC is
  already unit-guarded (TestRenderFiles_FullyOverriddenEnvFileStillListed); an e2e needs a NEW
  'override' service in the blueprint — a fixture-shape change. Defer.
- **COMPOSE_PROFILES multi-value**: needs a 2nd profiled service in the blueprint. Defer (low).
- **`--trace`/`--value` without `--var`** silently falls to the chain view (does NOT error).
  This is a PROD UX decision (go-engineer), not a test gap — flag to user; do NOT pin current
  behavior with a test. Deferred pending a decision.
- **No-docker unit test of `validate`** (so it's not dark in docker-less CI): needs a prod seam
  to mock the `docker compose` exec. Deferred (prod refactor).
- **compose-go version canary** test: low; deferred.

## Notes
- Acceptance count currently 79; this batch adds ~11 assertions (qa reconciles + bumps header).
- Use existing examples/monorepo where marked; scratch tmpdir fixtures for D-group + E +
  A-negative (NEVER edit the committed fixture, NEVER seed the git-tracked service dotfiles).
- TTY-colored positive output is structurally untestable here (non-TTY harness) — don't claim it.
