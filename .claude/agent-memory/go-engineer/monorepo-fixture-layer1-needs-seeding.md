---
name: monorepo-fixture-layer1-needs-seeding
description: examples/monorepo Layer-1 root .env/.dev.env/.secrets.env do NOT exist on disk (gitignored) — only example.* templates; run `cenvkit init` first or Layer-1 is empty
metadata:
  type: project
---

In `examples/monorepo/`, the Layer-1 root dotfiles (`.env`, `.dev.env`,
`.prod.env`, `.secrets.env`) are NOT committed — they are gitignored. Only the
`example.*` templates exist on disk. So a bare `cenvkit env-files` run from the
unseeded fixture prints ONLY the Layer-2 engine files (web/api/reports), with no
Layer-1 entries — and that is CORRECT skip-missing behavior, not a bug.

**Why:** The fixture mirrors a real repo where secrets/local env are gitignored
and created from templates. `.docker-env-chain` lists `.env`/`.${ENV}.env`/
`.${HOSTNAME}.env`/`.secrets.env`; chain.Resolve silently skips the missing ones.

**How to apply:** To verify the full Layer-1+Layer-2 merge against the fixture,
FIRST seed it — but NEVER modify the committed `examples/monorepo` (it is the
frozen parity reference, off-limits). Copy it to a `mktemp -d`, run
`cenvkit --project-dir <tmp> init` to seed `.env`/`.dev.env`/etc from `example.*`,
then run `cenvkit env-files` there. Also: a binary built into a temp dir OUTSIDE
the module can't `go run` (no go.mod up-tree) — `go build -o <tmp>/cenvkit ./cmd/cenvkit`
once and exec that. Verified: seeded dev run emits `.env, .dev.env` then Layer-2;
`COMPOSE_ENV=prod` swaps to `.prod.env` + `.web.prod.env` (interpolated
COMPOSE_FILE overlay). Related: qa's `TestResolve_MonorepoFixture_CrossSubproject`
only asserts Layer-2 (.web.env/.api.env), so it passes without seeding.
See [[resolvecomposefiles-standard-name-fallback]].
