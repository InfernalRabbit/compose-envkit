---
name: monorepo-fixture-needs-seeding
description: examples/monorepo ships only example.* + Layer-2 dotfiles; Layer-1 .env/.dev.env/.secrets.env do NOT exist on disk — acceptance must copy-to-temp + seed
metadata:
  type: project
---

`examples/monorepo/` (the acceptance fixture) contains on disk: `.docker-env-chain`,
`example.env`/`example.dev.env`/`example.prod.env`, and the Layer-2 service
dotfiles (`web/.web.env`, `api/.api.env`, `services/reports/.reports.env`). It
does NOT contain `.env`, `.dev.env`, `.prod.env`, or `.secrets.env` — the Layer-1
files the `.docker-env-chain` references. So `chain.Resolve` against the raw
fixture yields ZERO Layer-1 files (all skipped-missing); only Layer-2 populates
COMPOSE_ENV_FILES.

The legacy `test/smoke-monorepo.sh` handles this by (lines ~99-121):
`cp -R examples/monorepo/. $WORK/`, run install.sh, then explicitly
`cp example.env .env`, `cp example.dev.env .dev.env`, `cp example.prod.env .prod.env`.

**Why:** secrets/dotenvs are gitignored by design; the committed fixture is
templates-only. Any acceptance harness that runs against the fixture in place,
without seeding, mis-tests (Layer-1 vars absent) AND can dirty the tracked repo
if it runs `cenvkit init` there.

**How to apply:** reviewing the acceptance port — require the Go harness to
MkdirTemp, copy the fixture in, and seed `example.* -> .*` (or run
`cenvkit init` in the temp copy) BEFORE asserting. Scenarios asserting Layer-1
overlay (`.dev.env`/`.prod.env`, IS_DEV — scenario 17) and the docker
`compose config` scenarios depend on this. NOTE: STACK_TIER is NOT a Layer-1
var — it comes from the `docker-compose.dev.yml`/`docker-compose.prod.yml`
compose overlays (COMPOSE_FILE selector), so it does not need the Layer-1 seed;
IS_DEV (from `example.dev.env`/`example.prod.env`) is the seed-dependent one.
Related: [[carried-bug-classes-cenvkit]].

**C4 recurrence (2026-06-19):** named-chain sections in `.cenvkit.envchain`
introduce NEW Layer-1 files (e.g. `[ci]` → `.ci.env`). qa created `.ci.env`
on disk locally but did NOT track it / add an `example.ci.env` template + a
`stageMonorepo` seed entry — so C4-3/C4-4 (`--chain ci`) pass locally but break
on a fresh checkout. The lead caught it via committed-tree discipline
(`git stash -u && go test`). Fix pattern = TRACKED `example.<x>.env` + a
`{example.<x>.env → .<x>.env}` entry in stageMonorepo's seed slice (test ~:149) +
delete the stray `.<x>.env`. **Reviewer cue:** whenever a fixture chain/section
references a NEW `.<name>.env`, grep `git ls-files` for a matching `example.<name>.env`
template AND confirm stageMonorepo seeds it — a dotfile present on disk but absent
from `git ls-files` is the tell. Verify via `rm -f the-dotfile && go test ./test/...`.
