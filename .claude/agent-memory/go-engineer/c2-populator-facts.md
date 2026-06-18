---
name: c2-populator-facts
description: Verified C2 populator facts — GetEnvFromFile returns file-vals-only, U1-vs-U2 divergence only on shell/later-file refs (monorepo has none), cobra ArgsLenAtDash dash-detection
metadata:
  type: project
---

C2 (`cenvkit run`/`env` + flatten) verified facts, probed 2026-06-19 against compose-go v2.11.0 + cobra v1.10.2.

**Why:** the populator's `--expand` flatten MUST match `docker compose` AND env-debug (parity test asserts it); the seam + unification decision hinge on exactly how `GetEnvFromFile` resolves vs provenance.go's incremental loop.

**How to apply:** when implementing `internal/engine` flatten + `run`/`env` in cmd, use these exact mechanics.

- **`dotenv.GetEnvFromFile(currentEnv, files)` (dotenv/env.go:25) returns ONLY parsed file values** — `currentEnv` is consulted solely as a `${VAR}` lookup source (per-file lookup = `currentEnv[k]` first, else accumulated `envMap[k]`), NEVER copied into the result. So the **shell-wins overlay is the populator's job** (probe-confirmed: `SHELL_X` absent from result). It **errors (not skips) on a missing file** → feed it ONLY chain.Resolve's existence-filtered list (MF2).
- **Full base available to EVERY file immediately** (unlike provenance.go:158-184, which builds the lookup incrementally and overlays shell only AFTER the file loop).
- **U1-vs-U2 divergence is real but narrow:** it ONLY manifests when a chain-file VALUE references a var supplied by the **shell** or by a **later** chain file. Probe: `.env` with `GREETING=hello-${SHELL_ONLY}` (SHELL_ONLY only in shell) → U1=`hello-world` (compose-correct), U2=`hello-` + a spurious "variable is not set" warning.
- **monorepo fixtures have ZERO such case.** The ONLY in-value ref in any chain fixture is `COMPOSE_FILE=docker-compose.yml:docker-compose.${COMPOSE_ENV}.yml` in `.env`, and `COMPOSE_ENV` is set EARLIER in the same file → U1 and U2 produce IDENTICAL values (`docker-compose.dev.yml`). So **re-baselining env-debug onto GetEnvFromFile (U1) changes zero current fixtures** — blast radius is the env-debug acceptance goldens only IF a future fixture adds a shell/later-file in-value ref.
- **`GetEnvFromFile` logs warnings UNCONDITIONALLY (logrus warn) and is UNSUPPRESSABLE** — it takes no logging option and no `LookupFn` (only the `base` map), unlike `dotenv.ReadFile(path, LookupFn)` which env-debug's per-file `parseDotEnv` used to silence chain-provided refs (provenance.go:41-42). Confirmed 2026-06-19: a chain value with a genuinely-unset bare `$word`/`${VAR}` (no default, not in base) emits `level=warning msg="The \"word\" variable is not set..."` to stderr AND resolves to empty — this is correct compose/`docker compose` PARITY (value-wise), but the WARNING now leaks on `cenvkit run`/`env` AND on post-U1 `env-debug` (since interpEnv routes through Flatten). It is a UX wart, NOT a value regression. Suppressing it would require globally lowering logrus level around the call (out of C2 scope; flagged to lead). `--no-expand` avoids it entirely (no expansion → no lookup → no warning).
- **cobra `ArgsLenAtDash()` (v1.10.2) cleanly detects `--`:** returns `>= 0` (count of args before `--`) when `--` present, **`-1` when absent** (fresh command per call — don't reuse a *Command, it caches). `args` already has everything before `--` stripped AND flag-parsing STOPS at `--` (so `run -- echo --foo` passes `--foo` through). So `run` does NOT need `DisableFlagParsing`+manual extract (that's only `compose`'s need, b/c docker has colliding flags): `ArgsLenAtDash() < 0` → `--` required (exit 2); `>=0 && len(args)==0` → empty post-dash (exit 2); else `args` IS the command.
- **`exec.ExitError` embeds `*os.ProcessState`**; `ProcessState.ExitCode()` = child code (or -1 if signaled). `syscall.WaitStatus.Signaled()`/`.Signal()` give the signal for 128+signo. Missing binary surfaces as a non-`*ExitError` (`exec.ErrNotFound`/`os.ErrNotExist` wrapped) → map to 127; non-executable → 126.

See [[v3-a-attribution-two-env-trap]] (interpEnv vs chainEnv), [[compose-go-api-facts]].
