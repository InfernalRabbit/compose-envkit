---
name: withconfigfileenv-no-interp-cwd
description: compose-go WithConfigFileEnv does NOT interpolate ${VAR} in COMPOSE_FILE and resolves relative paths against process cwd (not WithWorkingDirectory) — engine overlay-selection bug
metadata:
  type: project
---

`cli.WithConfigFileEnv` (compose-go v2.11.0, source `cli/options.go:137`) reads
the RAW `COMPOSE_FILE` value, `strings.Split`s on the separator, and calls
`absolutePaths` = `filepath.Abs` + `os.Stat` on each piece. Two consequences,
both probe-confirmed against `examples/monorepo` (probe `/tmp/cenvkit-cf-probe`):

1. **No interpolation.** `${VAR}` inside COMPOSE_FILE is stat'd literally.
   monorepo `example.env:18` `COMPOSE_FILE=docker-compose.yml:docker-compose.${COMPOSE_ENV}.yml`
   → engine loads ONLY the base file; the `${COMPOSE_ENV}.yml` overlay is dropped.
2. **cwd-relative, not WithWorkingDirectory.** `absolutePaths` uses `filepath.Abs`
   (process cwd), so even a LITERAL relative COMPOSE_FILE is dropped unless cenvkit
   runs from the project dir.

**Why:** the spec/plan rely on `WithConfigFileEnv` to "honor COMPOSE_FILE"
(spec §4 step 3 line 117; plan Task3 Step9). The honoring is incomplete for
interpolated/relative values — a divergence from how the real `docker compose`
(which interpolates COMPOSE_FILE from its auto-loaded .env) behaves.

**How to apply:** when reviewing engine COMPOSE_FILE handling, require the manual
primary path: interpolate COMPOSE_FILE from in.Env, split on
COMPOSE_PATH_SEPARATOR-or-os.PathListSeparator, join-to-abs against ProjectDir,
pass as `configs` to NewProjectOptions (verified to load the overlay). Fix found
working in probe.

**Seam-drift trap (verified):** scenario 15 (`cenvkit compose config`) runs via
`exec docker compose` with only COMPOSE_ENV_FILES injected (plan lines 1419-1420,
1504-1505); docker compose interpolates COMPOSE_FILE itself, so scenario 15 stays
GREEN regardless of the engine bug. A RED guard for this must hit the engine
directly (`env-files` / `env-debug --files`) with a fixture whose overlay (selected
only via interpolated COMPOSE_FILE) adds an env_file the base lacks. Current
monorepo overlays add only `environment: STACK_TIER` (no env_file/new service), so
no existing assertion regresses — the bug is latent until an overlay carries an
env_file. See [[has-compose-file-gate-seam]], [[compose-go-option-order-and-compose-file]].
