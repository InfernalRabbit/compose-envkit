---
name: compose-go-option-order-and-compose-file
description: compose-go cli ProjectOptionsFn are order-sensitive; WithConfigFileEnv reads o.Environment so WithEnv must precede it; and it does NOT interpolate COMPOSE_FILE — handle manually
metadata:
  type: project
---

compose-go v2.11.0 `cli.NewProjectOptions` applies `ProjectOptionsFn` **in the
order passed** (verified: `cli/options.go` NewProjectOptions loops `for _, o := range opts`).
Two order/behavior traps confirmed by live probe against v2.11.0:

1. **`cli.WithConfigFileEnv` reads `o.Environment[COMPOSE_FILE]`**, and
   `o.Environment` starts EMPTY (compose-go deliberately does not expose OS env).
   So `cli.WithEnv(env)` MUST be passed BEFORE `WithConfigFileEnv`/`WithDefaultConfigPath`,
   or COMPOSE_FILE is silently ignored and the loader falls back to default
   discovery (`compose.yaml`/`docker-compose.yml`). Probe: plan's option order
   (config-first) → `COMPOSE_FILE=base.yml:docker-compose.prod.yml` dropped,
   only default `compose.yaml` loaded.
2. **`WithConfigFileEnv` does NOT interpolate `${VAR}` inside COMPOSE_FILE.** It
   splits on the path separator and stats the RAW string. So a value like
   `docker-compose.${COMPOSE_ENV}.yml` (the cenvkit monorepo fixture's
   `example.env:18`) is stat'd literally and errors / is dropped. The engine must
   manually: read COMPOSE_FILE from in.Env, interpolate ${COMPOSE_ENV}/${ENV},
   split on `:` (or COMPOSE_PATH_SEPARATOR), join-to-abs against ProjectDir, and
   pass as the `configs` arg to NewProjectOptions. Probe: manual handling →
   `[api web]` (both base+overlay); WithConfigFileEnv path → only base.

Separator: compose-go uses `COMPOSE_PATH_SEPARATOR` env if set, else
`os.PathListSeparator` (`:` on unix). It never defaults to `,`.

**Reorder ALONE is insufficient (re-probed 2026-06-15).** `WithConfigFileEnv`
calls `absolutePaths`, which does `filepath.Abs` (against the PROCESS cwd) +
`os.Stat` on each split COMPOSE_FILE entry — it ignores `o.WorkingDir`. So even
with `WithEnv` first, a relative COMPOSE_FILE entry stats against cwd and
`NewProjectOptions` ERRORS (`stat <cwd>/docker-compose.yml: no such file`) when
cwd != ProjectDir. Only the manual-handling path (interpolate, join-to-abs
against ProjectDir, pass as `configs`) actually loads base+overlay. Treat any
"just reorder the options" fix as incomplete; the manual COMPOSE_FILE branch is
mandatory.

**Why:** This is the load-bearing seam for cenvkit scenario 15 (dev/prod overlay)
and scenario 8 (prod). A green per-package unit test misses it because the engine
test fixtures use default discovery, not COMPOSE_FILE.

**How to apply:** When reviewing any `internal/engine` compose-go wiring, check
(a) WithEnv precedes config-file options, and (b) COMPOSE_FILE interpolation is
handled in cenvkit code, not delegated to WithConfigFileEnv. Relates to
[[carried-bug-classes-cenvkit]] (seam drift class).
