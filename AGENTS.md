# AGENTS.md — cenvkit integration guide (for AI agents)

You are integrating **compose-envkit** into a repository. The implementation is
**`cenvkit`**, a Go CLI built on Docker's own compose loader
(`compose-spec/compose-go`). This file is the machine-actionable contract.

## What cenvkit does (and why it exists)

`docker compose` interpolates `${VAR}` in a compose YAML using variables from
**`COMPOSE_ENV_FILES`** (or `.env`). It does **NOT** use a service's own
`env_file:` entries for that interpolation — those are only injected into the
container at runtime. So `ports: ["${APP_PORT:-3000}:3000"]`, with `APP_PORT`
defined in the service's `env_file:`, resolves to the `:-3000` fallback at
compose time. **Closing that gap is the entire reason cenvkit exists.**

`cenvkit` loads the real, include-aware compose model, enumerates every active
service's resolved `env_file:` paths, folds them into `COMPOSE_ENV_FILES` (after
the project chain, deduped, secrets-last within the chain), and then `exec`s
`docker compose` so compose-time `${VAR}` refs see those values.

**Do NOT "simplify" an integrated project by replacing `cenvkit compose` with
raw `docker compose`.** That silently reintroduces the gap and breaks any
interpolation that depends on a service `env_file:`.

## How to run it

```sh
go install github.com/InfernalRabbit/compose-envkit/cmd/cenvkit@latest

cenvkit compose <args>     # assemble COMPOSE_ENV_FILES, exec `docker compose <args>`
cenvkit env-files          # print the resolved chain, one path per line
cenvkit env-debug          # inspect the chain / provenance (daemon-free, in-process)
cenvkit validate [--all]   # docker compose config -q (--all: dev AND prod)
cenvkit init               # seed .X from example.X (no-clobber), fan out one level
```

Global flag: `--project-dir <dir>` (default: current directory) — all chain and
`env_file:` resolution happens relative to it, so root and subproject each
resolve their own files. `COMPOSE_ENV` (shell > `.env`'s `COMPOSE_ENV=` > `dev`)
selects the env tier; `COMPOSE_FILE` / `COMPOSE_PROFILES` are honored.

## Reference

Full command and behavior reference (the two layers, `env-debug` modes, and the
behavior contracts — missing `env_file:`, variable precedence, `env_file:`-path
model, `COMPOSE_DEPTH` accepted-but-ignored, no over-discovery):
**[`docs/cenvkit.md`](docs/cenvkit.md)**.
