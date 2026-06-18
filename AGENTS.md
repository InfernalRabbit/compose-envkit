# AGENTS.md — cenvkit integration guide (for AI agents)

You are integrating **compose-envkit** into a repository. The implementation is
**`cenvkit`**, a Go CLI built on Docker's own compose loader
(`compose-spec/compose-go`). This file is the machine-actionable contract.

## What cenvkit does (and why it exists)

`docker compose` interpolates `${VAR}` in a compose YAML using variables from
**`COMPOSE_ENV_FILES`** (or `.env`). It does **NOT** use a service's own
`env_file:` entries for that interpolation — those are only injected into the
container at runtime. So `ports: ["${APP_PORT:-3000}:3000"]`, with `APP_PORT`
defined only in the service's `env_file:`, resolves to the `:-3000` fallback at
compose time (docker/compose#3435, open since 2016 and never fixed upstream).

`cenvkit` does two coherent things:

1. **Gap-debugger (its reason to exist).** It keeps each service's `env_file:`
   **runtime-only** and detects the gap above. `cenvkit env-debug` explains it with
   provenance, **daemon-free** (in-process compose-go, no Docker daemon), and
   **`cenvkit gap-report`** is a CI/pre-build lint — exit **1** if any `${VAR}` is
   satisfied only by a service `env_file:`, **0** clean, **2** if no compose file is
   found.
2. **Env-chain populator.** It assembles the **Layer-1 project chain**
   (`.cenvkit.envchain`: tokenized, ordered, secrets-last) and delivers it: to
   `docker compose` (`cenvkit compose` → `COMPOSE_ENV_FILES`), to any process
   (`cenvkit run -- <cmd>`, no docker), or as text (`cenvkit env`). The chain is the
   place for values that should feed `${VAR}` interpolation.

**Rule for agents:** if a value is referenced as `${VAR}` in compose YAML, it must
live in the **Layer-1 chain** (`.env`/`.<env>.env`/`.secrets.env`), NOT only in a
service `env_file:`. Detect gaps with `cenvkit gap-report` (CI) or
`cenvkit env-debug --trace --var <V>` (`"gap": true` in `--json`). A service
`env_file:` value is for the container only.

**Do NOT "simplify" by replacing `cenvkit compose` with raw `docker compose`** — you
lose the Layer-1 chain assembly (tokens, ordering, secrets-last, `COMPOSE_FILE`
overlay/interpolation) and the gap protection.

## How to run it

```sh
go install github.com/InfernalRabbit/compose-envkit/cmd/cenvkit@latest

# debug the gap (the moat)
cenvkit gap-report             # CI lint: exit 1=gaps / 0=clean / 2=no compose file (+ --json)
cenvkit env-debug              # inspect the chain / provenance (daemon-free)

# deliver the chain to a consumer
cenvkit compose <args>         # assemble COMPOSE_ENV_FILES, exec `docker compose <args>`
cenvkit run -- <cmd> [args]    # exec any process with the merged env (no docker)
cenvkit env [--format dotenv|json|shell]   # emit the merged env for CI / scripts / eval

# chain utilities
cenvkit env-files              # print the resolved COMPOSE_ENV_FILES chain, one path/line
cenvkit validate [--all]       # docker compose config -q (--all: dev AND prod)
cenvkit init                   # seed .X from example.X (no-clobber), fan out one level
```

`run` / `env` flags: `-e <env>` (override `CENVKIT_ENV` for this invocation);
`--expand` (default — expand `${VAR}` / `${VAR:-def}` exactly like compose) vs
`--no-expand` (emit literally). `run` also takes `--print` (dump the env, no exec)
and requires `--` before the command. `cenvkit env --expand` is byte-identical to
what `cenvkit compose` / `docker compose config` interpolate.

## Chain file, selector, named chains

- Chain file: **`.cenvkit.envchain`** (one path template per line; `#` comments;
  optional `[name]` sections). Built-in default when absent: `.env`,
  `.${CENVKIT_ENV}.env`, `.secrets.env`.
- Selector: **`CENVKIT_ENV`** — shell `CENVKIT_ENV` > `.env`'s `CENVKIT_ENV=` >
  `"dev"`. Token `${CENVKIT_ENV}` (alias `${ENV}`) substitutes in chain templates +
  `COMPOSE_FILE`. **`COMPOSE_ENV` and `.docker-env-chain` are removed — no
  back-compat.** (`COMPOSE_ENV_FILES` is the real Docker Compose variable, untouched.)
- **Named chains:** `--chain <name>` (a persistent flag on every command) selects a
  `[name]` section of `.cenvkit.envchain`; the default is the header-less /
  `[default]` chain. Sections are standalone (no inheritance) and orthogonal to
  `CENVKIT_ENV`. An unknown `--chain` exits **2** and lists the available names.

## Secrets — out of scope

cenvkit does **not** manage, mask, or encrypt secrets. `.secrets.env` simply loads
**last** in the chain (last-wins); nothing is written to disk. For real secret
management, wrap cenvkit: `sops exec-env -- cenvkit run -- <cmd>`.

## Global flags & env-debug modes

`--project-dir <dir>` (default: current directory), `--chain <name>`,
`--color auto|always|never`. `env-debug` modes: `--list` (chain files — the default
view), `--effective [--service S]`, `--files`, `--trace --var V`, `--value --var V`,
`--overview`; add `--json` to any for the structured report. Output is colored on a
TTY, plain when piped / `--json` / `NO_COLOR` / CI.

## Reference

Full command + behavior reference: **[`docs/cenvkit.md`](docs/cenvkit.md)**.
Design + history: **[`docs/superpowers/`](docs/superpowers/)**.
