# cenvkit — generic populator + foregrounded compose gap-debugger (design)

Status: **DESIGN v3 (brainstorm-approved 2026-06-19; revised after adversarial
spec-review `wf_1573eeb8-dce`; cycle order + rename + selector decisions locked
2026-06-19 — see §3/§12; chain file named `.cenvkit.envchain`; pending final spec review).** Preserves the Layer-1
chain model, the compose-go engine seam, the daemon-free env-debug, and the
`2026-06-17-cenvkit-layer2-debug-only-design.md` run-path contract (run path =
Layer-1 only; service `env_file:` runtime-only).

Sources backing the decisions and the verified facts cited below:
- Landscape scan (17-agent, adversarially verified): `wf_96edac1c-864` (2026-06-18);
  architect memory `competitive-landscape-positioning.md`.
- Spec adversarial review (5 critics + verification against real code): `wf_1573eeb8-dce`
  (2026-06-19). All `file:line` facts below were verified there.
- Library: `go.mod` (`compose-go v2.11.0`); `go doc …/v2/dotenv`
  (`GetEnvFromFile(currentEnv, files)`, `ReadFile(path, LookupFn)`);
  `internal/engine/provenance.go:41-48`.

## 1. Context, positioning & mission

cenvkit assembles a Layer-1 project env chain (pure Go) into `COMPOSE_ENV_FILES`
and execs `docker compose`, and ships a **daemon-free `env-debug`** that loads the
real compose model in-process (compose-go, isolated in `internal/engine`) and
**detects the `env_file:`→`${VAR}` interpolation gap**.

A verified landscape scan decomposed cenvkit's value into three capabilities:
(1) **layered-chain delivery** — not novel (`mise`, `dotenvx`, `op run`, `sops`,
compose `--env-file`); (2) **chain debugging *without* compose** — not novel, cenvkit
behind (`mise`, `Dynaconf`); (3) **chain debugging *with* compose** — source-attributed
provenance + `env_file:`→`${VAR}` gap-detection — **NOVEL, uncontested**
(`docker compose config --environment` is a flat dump with no source attribution;
everything else is compose-blind; Docker closed #13190 *not planned*, #3435 open since
2016 → the gap is permanent by design).

> **Mission.** cenvkit delivers the layered env chain to its consumer — `docker
> compose` or a plain process — and debugs that chain with and without compose, with
> the Docker Compose `env_file:`→`${VAR}` **gap-debugger as its differentiating core**.

- **Core / moat:** `env-debug` + a `gap-report` CI/pre-build lint.
- **Local arm (equal, not headline):** `run` / `env` — the same chain, same values,
  for no-docker development; the explicit goal is **one tool, not two**, NOT to compete
  with `mise`. Honestly described in docs as a thin convenience.
- **Honest boundary:** a team that never relies on `env_file:` for `${VAR}` may not need
  cenvkit. Real audience: monorepos with many services and `env_file:` chains.

## 2. Decision summary (locked)

- **Generic populator** (`cenvkit run`, `cenvkit env`): assemble Layer-1 → flatten to a
  merged `KEY=VALUE` map → exec a process / emit. The no-docker "local arm."
- **Value expansion via `compose-go`'s `dotenv` package** (already a dep). `--expand`
  default; `--no-expand` literal. **One expansion path** shared with env-debug (§5, MF4).
- **Gap-debugger foregrounded + a `gap-report` lint** that **exits non-zero on gaps**,
  runs **pre-build**, daemon-free. (Resolves the 2026-06-17 "warning vs non-zero" open
  question: the *lint* is non-zero; `env-debug` stays informational/exit-0.)
- **Secrets fully OUT of scope:** no masking, no redaction, no encryption, no backend.
  Secrets = plaintext `.secrets.env`, loaded last. External managers wrap cenvkit
  (`sops exec-env -- cenvkit run …`). cenvkit never hides values.
- **Named chains (Cycle 4):** N standalone named chains in `.cenvkit.envchain`, picked by
  `--chain <name>`, orthogonal to `CENVKIT_ENV`; still a file-list, **NOT** the declined
  `cenvkit.yaml`.
- **Upstream-first / thin preserved:** the compose path still hands compose a file
  *list* and lets compose own resolution; only the run/emit path flattens, via compose-go.

**Decided (2026-06-19):** the chain-file + selector rename ships as **Cycle 3** —
`.cenvkit.envchain` primary (alias `.docker-env-chain`), `CENVKIT_ENV` primary (alias
`COMPOSE_ENV`). **Decided OUT:** an env-only `cenvkit.yaml`
(was tied to the dropped task-runner; the chain is configured by the chain file +
`CENVKIT_ENV`). Also OUT: task-runner/commands, plugin/target platform, k8s,
non-dotenv env formats.

## 3. Decomposition into three sequenced cycles

The review confirmed the design is implementable but **must be decomposed** — three
independently-shippable changes with different blast radii. Each is its own
plan → build → review → integrate cycle (one squashed commit per cycle).

| Cycle | Scope | Risk | Why this order |
|---|---|---|---|
| **C1 — `gap-report`** | Thin non-zero-exit lint over the **already-existing** gap logic (`provenance.go:424`) + shared JSON schema | Lowest | Highest moat value, almost no new surface; ships the differentiator first |
| **C2 — populator** | New `internal/envmap` + `run` / `env` (+ the one-engine unification, MF4) | Medium | The genuine new fidelity surface; most must-fixes live here |
| **C3 — rename** | `.cenvkit.envchain` / `CENVKIT_ENV` + back-compat aliases (`.docker-env-chain`, `COMPOSE_ENV`) across every chain-reading command + fixtures | Cross-cutting | Confirmed (Q1=yes); precedes C4 |
| **C4 — named chains** | `[name]` sections in `.cenvkit.envchain` + `--chain <name>` selector across all commands | Additive | Flexibility layer; extends the renamed file format; last |

**Cycle order (decided 2026-06-19): C1 → C2 → C3 → C4** — moat-first; every cycle is
**additive** (later cycles add flags/format without changing prior default behavior). C3
(rename) precedes C4 (named chains, which extends the renamed file format).

## 4. Shared architecture

```
cmd/cenvkit/         CLI (cobra) — adds `run`, `env` (C2), `gap-report` (C1); foregrounds env-debug
internal/chain/      Layer-1 assembly (pure Go). C3: .cenvkit.envchain alias + CENVKIT_ENV token. C4: parse [name] sections.
internal/envfiles/   EXISTING (assemble.go) — Layer-1 COMPOSE_ENV_FILES join for `compose`. UNCHANGED.
internal/envmap/     NEW (C2). Flatten ordered EXISTING file list → merged KEY=VALUE. ADDITIVE,
                     does NOT replace envfiles. --expand: compose-go dotenv; --no-expand: reuse
                     provenance.parseOrderedLiteral. Shell-wins overlay.
internal/engine/     compose-go seam (env-debug model, gap-detection, validate). C1 adds the
                     gap-report query (reuses the EXISTING gap set at provenance.go:424).
internal/provenance/ env-debug model + render (pure Go). UNCHANGED (C1 reuses its gap fields/JSON).
internal/bootstrap/  init. internal/style/ styling. UNCHANGED.
```

`run`/`env` need NO compose project, NO `COMPOSE_FILE` discovery, NO docker.

## 5. Cycle 2 — generic populator (`run` / `env` / `internal/envmap`)

### 5a. Chain selection & resolution (shared with all commands)

- **Selector precedence (pinned; bare `ENV` dropped as a *source* for safety):**
  shell `CENVKIT_ENV` › shell `COMPOSE_ENV` › `.env` `CENVKIT_ENV=` › `.env`
  `COMPOSE_ENV=` › `dev`. The `${ENV}` token remains a *template-substitution alias*
  for the resolved value, but the bare `ENV` env var does **not** select (too common a
  shell var). Alias logic lives in `chain.resolveComposeEnv` / `readChainTemplates`.
  (Decided 2026-06-19: bare `ENV` is **not** a selection source.)
- **`-e/--env ENV` flag (NEW):** injects the selector as a chain overlay exactly as
  `validate --all` does (`main.go:228-229` appends `COMPOSE_ENV=<v>`); an explicit
  `-e` wins over an inherited shell selector. `--project-dir` (existing persistent
  flag) selects the dir; `-C` MAY be added only as its shorthand — **no new dir flag.**
- **Existence-filter invariant (MF2):** `chain.Resolve` is the single point that drops
  non-existent templated entries (`chain.go:177-178` `continue` on stat failure).
  `internal/envmap` is fed **only** that existence-filtered list and must never receive
  a missing path (unit-tested). A file vanishing between resolve and read (TOCTOU) is
  **fatal**, reported with its path; `run`/`env` exit non-zero.

### 5b. Flatten (`internal/envmap`) — the corrected algorithm (MF1)

`dotenv.GetEnvFromFile(currentEnv, files)` returns **only the parsed file values**;
`currentEnv` is consulted **solely** as a `${VAR}` lookup source and is NOT in the
result (verified, compose-go v2.11.0 `dotenv/env.go`). So `base` has **two distinct
roles** and must not be conflated:

1. `chainVals` (`--expand`) `:= dotenv.GetEnvFromFile(process-env-as-map, chainFiles)`
   — files merge **last-wins**; in-file `${VAR}` resolves against process env +
   already-accumulated chain values (full base available to every file).
   - `--no-expand`: `chainVals :=` per-file **`provenance.parseOrderedLiteral`** (no
     third reader, MF/SF), merged last-wins; values verbatim (no `${…}` expansion).
2. **Final environment (shell-wins overlay)** — mirrors compose's `cli.WithDotEnv`
   doing `o.Environment.Merge(GetEnvFromFile(...))`, where `Mapping.Merge`
   (`types/mapping.go:183`) adds a key **only `if _, set := m[k]; !set`**:
   ```
   final := clone(process-env)
   for k, v := range chainVals { if _, set := final[k]; !set { final[k] = v } }  // shell wins
   ```
   `GetEnvFromFile` does **not** do this overlay — `envmap` must. Shell-wins applies
   **identically on `--expand` and `--no-expand`** (`--no-expand` only suppresses
   `${VAR}` expansion, MF/SF).

- **Unset `${VAR}` with no default → empty** (compose-go default; parity with `docker
  compose`), not an error. dotenv quote semantics still govern per-value expansion.

### 5c. One expansion path (MF4 — parity-critical)

`run`/`env` and `env-debug`/`gap-report` **must** resolve `${VAR}` **identically**, or
the same chain yields different values across commands. (Verified divergence:
`provenance.go:158-178` builds its lookup **incrementally** and overlays shell **after**
the file loop — its own comment admits first-pass cross-file/shell refs may not resolve
— whereas `GetEnvFromFile` exposes the full base immediately.)

- **Constraint:** all chain flattening routes through **one** expansion primitive.
- **Mechanism chosen in the C2 plan** (recommended: route the populator through the
  same per-file lookup the engine uses, OR migrate the engine to `GetEnvFromFile` and
  **re-baseline** its env-debug acceptance to the more compose-faithful values).
- **Enforced by a parity acceptance test:** `env --expand` == `env-debug --effective`
  == `docker compose config` for (a) a shell-only `${VAR}`, (b) an earlier-chain-file
  `${VAR}`, (c) `${VAR:-def}` with VAR set and unset.

### 5d. `run` — exec semantics

- Surface: `run [--project-dir DIR] [-e ENV] [--expand|--no-expand] [--print] -- <cmd> [args]`
  (`--expand` default). **`--` is required**; zero post-`--` tokens → usage error
  **exit 2**. Parser uses cobra `ArgsLenAtDash`/`DisableFlagParsing` so cenvkit's own
  flags parse but everything after `--` passes verbatim (mirrors `compose`'s manual
  extraction, `main.go:160`).
- **Exec model:** use `exec.Command` with explicit signal forwarding (or `syscall.Exec`;
  the plan picks one — `syscall.Exec` gives free signal/exit fidelity but then `--print`
  must run before the exec).
- **Exit-code mapping (POSIX parity, MF6):** child `*exec.ExitError` → its code;
  **missing binary → 127**, non-executable → 126, signal-terminated child → 128+signo.
- `--print`: dump the chain-derived env (identical content/format to `env --format
  dotenv`, see §5e), **skip exec, always exit 0**. Reveals plaintext secrets by design.

### 5e. `env` — emit semantics

- Surface: `env [--project-dir DIR] [-e ENV] [--expand|--no-expand] [--format dotenv|json|shell]`.
- **Emit key set (SF):** `env` emits **only chain-derived keys** with the shell overlay
  applied to those keys (bounded, reproducible — does **not** dump the entire inherited
  process env into CI logs). `run`, by contrast, execs with the full merged env. Output
  is **key-sorted** (`chain.Resolve` already sorts, `chain.go:207`) for stable goldens.
- **Quoting/escaping (MF8 — correctness + `eval` safety):**
  - `shell` (`export K=V`): single-quote the value, escape embedded `'` via the
    `'\''` idiom; refuse keys that aren't valid shell identifiers.
  - `dotenv` (`K=V`): quote values containing space/`#`/newline; escape `"` `$`
    backslash newline so output **round-trips** through compose-go's own parser.
  - `json`: standard JSON string escaping.
  - Unit tests: values with space / newline / `"` / `'` / `$` under each format.
- **Empty/all-missing chain:** emit no lines, **exit 0** (a fresh repo is legitimate;
  optionally mirror the env-debug empty-chain hint, commit `2019dd8`).

## 6. Cycle 1 — `gap-report` (the moat, thin)

A daemon-free **CI/pre-build lint** over the **existing** gap set.

1. Resolve chain + load the compose model daemon-free (existing `engine.Provenance`:
   `cli.WithoutEnvironmentResolution` + `cli.WithEnv(Layer-1-only)` so an `env_file:`-only
   `${VAR}` resolves to its `:-default`, matching the real run).
2. Gap set = the **existing** `referenced && !InChain && len(RuntimeDefs) > 0`
   (`provenance.go:424`). No new detection logic.
3. **Standalone `gap-report` verb** (decided, resolves Q2) — a distinct exit-code
   contract that does NOT overload `env-debug`'s exit-0 stance — but implemented over
   the **identical** gap set and a **shared JSON schema** so lint and inspector can
   never disagree.
4. **Exit codes (MF5):** gaps found → **1**; clean → **0**; **no compose file
   discovered** (empty `resolveComposeFiles` and no `COMPOSE_FILE`) → **2** with a clear
   "no compose file found" message (NOT a silent exit-0 pass — `provenance.go:137,261`
   currently early-returns chain-only/zero-gaps, which would mislead a CI lint).
5. **`--json` schema (SF):** stable, e.g.
   `{"gaps":[{"var","service","field","fallback","fix"}],"count":N}`, reusing
   `provenance` `RuntimeDef`/`Effect` field names; drives the non-zero exit independently
   of `--json`.
6. **Daemon-free** — explicitly needs NO docker daemon; runs before `docker build` /
   `docker compose build`.

## 7. Cycle 3 — rename / back-compat (confirmed; ships last)

- Chain file: `.cenvkit.envchain` primary; **`.docker-env-chain` fallback only if
  `.cenvkit.envchain` absent — primary wins, no merge** (`readChainTemplates` currently opens only
  `.docker-env-chain`, `chain.go:131`); both-present unit test.
- Selector token: `CENVKIT_ENV` primary; `COMPOSE_ENV` alias (and the `${ENV}` template
  alias of §5a). Same primary-wins rule.
- Touches every chain-reading command + the `examples/monorepo` fixtures + acceptance.
  Fully back-compatible; no removal date. **Confirmed (Q1 = yes); ships as the final cycle.**

## 7b. Cycle 4 — named chains (additive flexibility layer)

Lets a project define **N named chains** in `.cenvkit.envchain` and pick one with
`--chain <name>`. Decided shape (user, 2026-06-19): **general flexibility — standalone
sections, NO inheritance from `[default]`, NO compose binding.** Stays inside the niche
(it parameterizes the *one* thing — the env chain — it is NOT a config/command file; the
declined `cenvkit.yaml` stays declined).

- **Format:** optional INI-style `[name]` headers in `.cenvkit.envchain`. Lines before any
  header (or a header-less file — i.e. a legacy `.docker-env-chain`) are the implicit
  `[default]` chain (back-compat). Each `[name]` is a **complete, standalone** ordered file
  list; **no inheritance** (YAGNI — an `extends` can come later if ever needed).
- **Selection:** `--chain <name>` added to `run`, `env`, `gap-report`, `compose`,
  `validate`, `env-files`, and the debug command. Default = `[default]` / the header-less
  list. Token substitution (`${CENVKIT_ENV}` …) applies within the chosen section as today
  → **orthogonal to `CENVKIT_ENV`** (`--chain api` × `CENVKIT_ENV=prod` are independent).
- **No compose binding:** a named chain selects only the env file LIST; it does NOT pick
  compose services/projects (that stays project-dir / `COMPOSE_FILE`).
- **Errors:** `--chain <name>` absent from the file → exit 2 with the list of available
  chain names. `--chain` on a header-less file resolves only `default`.
- **Parsing:** extend `chain.readChainTemplates` to recognize `[name]` headers (pure Go, no
  new deps).
- **Flag-name collision (open detail for the C4 plan):** `env-debug` already uses `--chain`
  as a *boolean mode* (print the Layer-1 chain files); the named-chain *selector* is a
  string `--chain <name>` — they cannot be the same cobra flag on that command. Lean:
  rename env-debug's boolean mode to a descriptive `--list`/`--files-chain` (pre-1.0,
  back-compat note) and keep `--chain <name>` as the **one** universal selector; finalize
  in the C4 plan.
- **Additive:** `run`/`env`/`gap-report` ship in C1/C2 without `--chain`; C4 adds the flag +
  section parsing with default behavior unchanged.

## 8. Library choice (verified)

`compose-go/v2/dotenv` (`GetEnvFromFile` / `ReadFile` + `LookupFn`), pinned `v2.11.0`,
already imported in `internal/engine/provenance.go:12`. It is the same primitive
compose-go's `cli.WithDotEnv` uses, so `${VAR}`/`${VAR:-default}`/nested defaults expand
exactly as `docker compose` does — no hand-rolled resolver. Pin deliberately; the
parity + gap-fidelity acceptance tests guard against silent upstream drift.

## 9. Testing

- **`internal/envmap` (unit):** last-wins ordering; `--expand` resolves
  `${VAR}`/`${VAR:-default}`/nested/literal-`${...}`; `--no-expand` == `parseOrderedLiteral`
  (export-prefix, inline-comment, quoted, empty-value fixtures); **shell-wins on both
  paths**; never handed a missing path; unset `${VAR}` → empty.
- **`run` (unit/accept, no-docker):** `--` enforcement + exit 2; exit-code mapping
  (127/126/128+signo, `*ExitError`); `--print` exits 0; signal forwarding.
- **`env` (unit, no-docker):** each `--format` quoting round-trips (space/newline/quote/`$`);
  chain-derived key set; sorted; empty chain → exit 0.
- **`gap-report` (accept):** seeded `env_file:`-only `${VAR}` → exit 1; clean → 0; no
  compose file → exit 2; `--json` schema stable.
- **Parity (docker-gated, MF4/SF):** a table — plain var; `${VAR:-def}` set/unset; shell
  override; special-char value; the gap var — asserting `env --expand` ==
  `env-debug --effective` == `docker compose config` for chain-scoped vars.
- **Back-compat (C3):** legacy `.docker-env-chain` + `COMPOSE_ENV`; both-files-present
  primary-wins.
- Full gate per CLAUDE.md before each cycle integrates (`gofmt -l .` empty,
  `go test ./...`, docker acceptance path).

## 10. Risks & mitigations

1. Populator looks redundant vs `mise`/native compose → position as the "local arm of the
   same chain"; honest docs; never marketed as a competitor.
2. `--expand` divergence from compose → **one** expansion path (§5c) + the parity test.
3. compose-go coupling for the moat → pin v2.11.0 (convention) + gap-fidelity acceptance
   test against the real loader.
4. Secrets in plaintext to stdout → documented stance; `--print`/`env` reveal values by
   design; **no `--mask`** (would be scope creep). UX footnote only.

## 11. Out of scope (declined or deferred)

Secret masking/redaction/encryption/backends (declined); task-runner / defined commands
/ `cenvkit.yaml` commands (dropped); env-only `cenvkit.yaml` (out — chain configured by
the chain file + `CENVKIT_ENV`); plugin/target platform + k8s (deferred); non-dotenv env
formats (declined, YAGNI). *Review confirmed these boundaries are held consistently
across the spec.*

## 12. Resolved decisions (2026-06-19) & remaining check

**Resolved with the user:**
- **Cycle order:** C1 `gap-report` → C2 populator → C3 rename → C4 named chains (moat-first).
- **Rename (§7):** Cycle 3 — `.cenvkit.envchain` primary, `.docker-env-chain` alias;
  `CENVKIT_ENV` primary, `COMPOSE_ENV` alias. **Chain-file name confirmed: `.cenvkit.envchain`.**
- **Selector (§5a):** bare `ENV` dropped as a selection source (kept only as the `${ENV}`
  template-substitution alias).
- **Named chains (§7b, Cycle 4):** general flexibility — standalone `[name]` sections, no
  inheritance, no compose binding; `--chain <name>` selector.

**Remaining open detail (deferred to the C4 plan):** the `--chain` flag-name collision with
`env-debug`'s existing `--chain` boolean mode (see §7b).

## 13. Documentation sync (final pass — after C1–C4)

One docs pass after the cycles land (NOT per-cycle, to avoid churn), in the architect zone,
each edit verified against the **shipped** CLI (no claims about unimplemented flags):
- **CLAUDE.md** (92 ln, exists) — reposition the project overview from "thin assemble
  `COMPOSE_ENV_FILES` + exec" to "**gap-debugger core + generic populator**"; add
  `run`/`env`/`gap-report`, the `.cenvkit.envchain`/`CENVKIT_ENV` naming + aliases, named
  chains, the secrets-OUT stance; refresh the verification-commands/module-boundary notes if
  the command set changed.
- **README.md** (227 ln, exists) — user-facing: **lead with the gap-debugger** (the moat) and
  the pre-build CI lint; document `run`/`env`/`gap-report`, named chains, the chain file +
  selector; keep the honest "composes with make/just, doesn't replace them" and "secrets via
  external tools (`sops`/`op`)" notes.
- **AGENTS.md** (58 ln, exists) — keep in lockstep with CLAUDE.md (same positioning/commands);
  it is the non-Claude agent-instructions mirror.

(GEMINI.md is absent — out of scope unless requested.)
