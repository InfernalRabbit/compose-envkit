# Portability

compose-envkit targets any POSIX-capable OS — Linux, macOS, the BSDs, and
Windows via WSL2 or Git-Bash. This page documents the guarantees, the Windows
story, and the BSD-vs-GNU caveats to keep in mind.

---

## POSIX guarantees

Every shipped script (`bin/docker`, `lib/*.sh`, `install.sh`, `test/*.sh`) is
`#!/bin/sh` and written to the POSIX shell + portable-awk baseline. The rules the
code holds itself to:

- **No bashisms.** No arrays, no `[[ … ]]`, no `${var,,}`, no process
  substitution `<(…)`, no `echo -e`. `local` is avoided. Output is via `printf`,
  not `echo` with flags.
- **Portable `awk` only** — no gawk-specific extensions. The `env_file:` parser
  and the debug-mode formatters run under the base `awk` (mawk/BSD awk/nawk).
- **Portable path resolution.** No GNU `readlink -f` and no `realpath` (BSD
  differs / they may be absent). Directories are resolved with the
  `CDPATH= cd -- "$(dirname -- "$0")" && pwd` idiom — works the same on every
  POSIX shell. `CDPATH=` neutralizes a user `CDPATH` that would make `cd` echo.
- **No GNU-only flags.** No `sed -i`, no GNU-only `grep`/`cut`/`find` options.
  `find -maxdepth` is used (universally supported on Linux/macOS/BSD `find`).
- **`set -eu`** in every engine script (the `--value` mode drops `-u` only inside
  the subshell where it sources env files, because env files legitimately
  contain bare `${REF}` that would be an "unbound variable" under `-u`).

Static verification is built in. `sh test/lint.sh`:

1. runs `sh -n` (POSIX syntax check) on every shipped script, and
2. runs `shellcheck -s sh -x` on them **if** shellcheck is on `PATH` (gracefully
   skipped — not failed — when it is absent).

`sh test/smoke.sh` is the end-to-end check: it installs the kit into a throwaway
project and asserts Layer-2 interpolation, every `env-debug` mode, and the
subproject path. It needs `docker compose` for the compose-touching assertions;
set `SMOKE_SKIP_DOCKER=1` to downgrade those to skips on a Docker-less box.

### What is *not* POSIX (by design)

- **`mk/*.mk`** target **GNU make** (see below) — they are not `sh` and are not
  linted as such.
- **`completions/env-debug.bash`** is a deliberate bash completion script (it
  uses `compgen`, `local`, `[[ … ]]`). It is never executed under `/bin/sh`;
  it's `source`d by an interactive bash/zsh shell. `env-debug.zsh` loads it via
  zsh's `bashcompinit`.

---

## GNU make vs BSD make

The `.mk` files are written for **GNU make** and rely on:

- `$(lastword $(MAKEFILE_LIST))` + `$(dir …)` to resolve the `scripts/` directory
  relative to each `.mk` file (so the include works from the root and from a
  vendoring subproject).
- `:=` immediate assignment, `?=` conditional assignment, the `$(if …)` and
  `$(shell …)` functions, `$(origin …)`, and `ifeq`.

On **BSD systems** (FreeBSD/NetBSD/OpenBSD, and macOS where the default `make` is
sometimes a BSD-flavored one), invoke **GNU make** explicitly — usually `gmake`:

```sh
gmake env-debug
gmake validate
```

macOS ships GNU make as `make` (3.81) by default, so `make` works there as-is; a
newer GNU make from Homebrew also works. BSD make (`bmake`/`pmake`) is **not**
supported for the make targets.

> The shell engine itself (`./docker`, `env-debug.sh`) does **not** need make at
> all — the `make` targets are thin wrappers around `sh scripts/env-debug.sh …`.
> If you can't get GNU make, call the scripts directly.

---

## Windows

Windows has no POSIX `sh`, so run the kit inside a POSIX environment:

- **WSL2 (recommended).** A full Linux userland; everything behaves exactly as on
  Linux. Run Docker Desktop with the WSL2 backend and use the kit from inside the
  WSL distro.
- **Git-Bash** (the MSYS2 `sh` bundled with Git for Windows). The scripts run; be
  aware of MSYS path translation (it rewrites `/c/...`-style paths) and that
  `docker compose` must be reachable from the Git-Bash `PATH`.

There is **no native PowerShell port** in this version — `bin/docker`,
`install.sh`, and `env-debug.sh` are POSIX shell only. Use WSL2 or Git-Bash.

---

## Docker Compose version floor

The Layer-2 `env_file:` discovery emits the long-form `env_file: required:` shape
and relies on compose understanding it, so target **Docker Compose ≥ 2.24.0**
(Jan 2024 — the release that added `env_file: required: false`). The kit was
developed against Compose v5.x.

The related native features the kit leans on landed earlier and are comfortably
covered by that floor: `COMPOSE_ENV_FILES` (v2.23.0), `include:` (v2.20.0), and
`DOCKER_DEFAULT_PLATFORM` (v2.13.0).

Modes that don't shell out to compose — `--chain`, `--diff`, `--files`,
`--value`, and `./docker env-files` — work with **no Docker at all**.

---

## Quick portability checklist

Before shipping the kit (or a change to it) to a new platform:

```sh
sh test/lint.sh                 # sh -n on every script; shellcheck if present
sh test/smoke.sh                # end-to-end (needs docker compose)
SMOKE_SKIP_DOCKER=1 sh test/smoke.sh   # the no-Docker subset

# on a BSD box, prefer:
gmake validate
```
