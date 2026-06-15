---
name: disableflagparsing-persistent-flag-leak
description: cenvkit `compose` subcommand uses DisableFlagParsing:true → persistent --project-dir leaks to docker compose and resolveProjectDir falls back to cwd; also missing dc.Dir vs legacy cd $PROJECT_DIR
metadata:
  type: project
---

The plan's `newComposeCmd` (plan §Task 6 Step 5, lines 1409-1432) sets
`DisableFlagParsing: true`. Probe-verified (cobra v1.10.2, the pinned version)
behavior:

- `cenvkit compose --project-dir X up`  → persistent flag `project-dir=""`
  (NOT parsed), and `--project-dir X` is forwarded in `args` to `docker compose`,
  which rejects it as unknown. `resolveProjectDir` silently falls back to cwd.
- `cenvkit compose up --project-dir X`  → flag DOES get `project-dir="X"` but the
  token STILL leaks into `args`. So behavior is position-dependent and leaks
  either way.

Separately, `newComposeCmd` never sets `dc.Dir`. Legacy parity ref
`lib/compose-env.sh:130-131` does `cd "$PROJECT_DIR"; exec docker compose "$@"`,
so docker compose must run in the resolved project dir, not cenvkit's cwd.

**Why:** the 61-assertion smoke suite does NOT trigger this — `run_shim` always
`cd`s into the dir (`test/smoke-monorepo.sh:139`) and never passes `--project-dir`
to `compose`. But the acceptance-port-plan (lines 114-115, 382) advertises
`cenvkit --project-dir <dir> env-files` as a supported invocation, so a
persistent flag that silently breaks on `compose` is a real UX/seam
inconsistency.

**How to apply:** when reviewing cobra subcommands with `DisableFlagParsing:true`,
flag any reliance on persistent/parent flags inside the RunE — they are NOT
populated. Recommend: strip a leading `--project-dir`/`--project-dir=` from args
before passthrough (or use `TraverseChildren`/Args splitting), set
`dc.Dir = resolvedProjectDir`, and document the contract. Related:
[[has-compose-file-gate-seam]], [[plan-consistency-defect-classes]].
