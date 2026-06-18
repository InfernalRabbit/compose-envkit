# Example monorepo restructure — kill the `include:`/`services:` conflict

**Author:** go-engineer · **Mode:** PLAN (read-only) · **Date:** 2026-06-19

## Problem (verified)

CI docker acceptance is RED. The masked error is:

```
services.web conflicts with imported resource
```

The root `examples/monorepo/docker-compose.yml` does TWO incompatible things at once:

1. `include:`s `web/`, `api/`, `services/reports/` — each subproject defines its
   own service (`web`, `api`, `reports`) in its own compose file.
2. ALSO redefines those SAME services under root `services:` to bolt on
   `networks: [shared]`, `depends_on`, and `environment.IS_DEV`.

Docker Compose deliberately does **not** merge an `include:`-imported service with
a same-named main-file service — it errors (docker/compose #11488, #11404). The
local engine here is v5.1.2 (anomalously tolerant — it does NOT error); CI is
v2.38.2 (strict) → red. Removing the redefinition is deterministically valid on
ALL versions because the conflict is structural: **no imported service is
redefined at the root any more.**

Verified locally (`examples/monorepo` staged to a tmp dir + seeded, driven by a
freshly-built `cenvkit`): the current root errors-as-tolerated on v5.1.2 (exit 0,
conflict silently merged); the proposed root produces identical resolved values
with the conflict structurally gone.

---

## The fix (minimal)

### 1. New root `examples/monorepo/docker-compose.yml`

`include:` stays (core to cenvkit's value); the root no longer redefines ANY
imported service. Only the root-OWNED `tools` service keeps `networks: [shared]`
(valid — `tools` is defined here, not imported). `IS_DEV` moves into `web/`'s own
file (see #2). The shared-network attach on web/api/reports and the
`api.depends_on.web` ordering are DROPPED (rationale below).

```yaml
# ============================================
# Root compose — the UNIFIED stack, run from the repo root.
#
# It `include:`s each subproject's own compose file. Each subproject defines its
# OWN service; the root does NOT redefine them (Docker Compose does not merge an
# imported service with a same-named root service — it errors. docker/compose
# #11488). Cross-cutting bits that only make sense for one subproject live in
# that subproject's file (e.g. web's IS_DEV tier flag). Each subproject still
# runs standalone from its own dir.
#
# Requires Docker Compose v2.24+ (for `env_file: required:` and `include:`).
#
# WHY the kit matters here: the included subproject compose files reference
# ${WEB_PORT} / ${API_PORT}, but those vars live ONLY in the subprojects' own
# service env_file: (web/.web.env, api/.api.env). Those env_file:s are
# runtime-only — they do NOT feed ${VAR} interpolation. So ${WEB_PORT} and
# ${API_PORT} fall back to :-0 at the run. Use `cenvkit env-debug --trace --var
# WEB_PORT` to see the gap; promote a port to the Layer-1 chain (.env) if you
# need it to interpolate at compose time.
# ============================================

include:
  - path: ./web/docker-compose.yml
  - path: ./api/docker-compose.yml
  - path: ./services/reports/docker-compose.yml   # nested one level deeper (services/<svc>/)

services:
  # Optional helper — OFF by default, enabled with COMPOSE_PROFILES=tools.
  # tools is root-OWNED (not imported), so it MAY join the shared network and
  # demonstrates compose-envkit's profiles passthrough unchanged (no kit state).
  tools:
    image: busybox
    profiles: [tools]
    command: ["sh", "-c", "echo tools; sleep 3600"]
    networks: [shared]

networks:
  shared:
    driver: bridge
```

> `networks: shared` is KEPT (referenced by `tools`). Leaving the top-level
> `networks:` block in is harmless even though web/api/reports no longer attach;
> it stays because `tools` uses it and the blueprint still narrates "a shared
> network for the stack".

### 2. `web/docker-compose.yml` — gains `IS_DEV` in its OWN `environment:`

`IS_DEV` is valid on `web` because `web` is *defined* in this file. The value
still comes from the Layer-1 chain via `${IS_DEV:-unset}` (`.dev.env`=true /
`.prod.env`=false) — the chain-interpolation demo (MF4-parity-2) is preserved.

```yaml
# (header comment unchanged; optionally add a line noting IS_DEV is the root tier
#  flag surfaced on web — lead/docs decision, not required for tests)
services:
  web:
    image: busybox
    command: ["sh", "-c", "echo web on $${WEB_PORT}; sleep 3600"]
    env_file:
      - path: ./.web.env
        required: false
      - path: ./.web.${CENVKIT_ENV:-dev}.env
        required: false
    ports:
      - "${WEB_PORT:-0}:80"
    environment:
      WEB_PORT: "${WEB_PORT:-0}"
      IS_DEV: "${IS_DEV:-unset}"        # <-- ADDED: root tier flag, chain-interpolated
```

### 3. No other file diffs needed

- `api/docker-compose.yml`, `services/reports/docker-compose.yml` — UNCHANGED.
- `docker-compose.dev.yml` / `docker-compose.prod.yml` — UNCHANGED. They re-open
  `web` to add `STACK_TIER`. This still WORKS: `web` is now defined ONLY in
  `web/docker-compose.yml`, so the overlay (`-f` layer, NOT an include) merges
  into it cleanly — no conflict (overlay merge ≠ include-conflict). Verified:
  `STACK_TIER: dev`/`prod` still render and still appear in `--effective`.
- `.cenvkit.envchain`, all `example.*` / `.env` files — UNCHANGED.

---

## Dropped cross-cutting bits — rationale

| Bit (current root) | Disposition | Rationale |
|---|---|---|
| `web.environment.IS_DEV: "${IS_DEV:-unset}"` | **MOVED** into `web/docker-compose.yml` | `web` is defined there; valid. Chain still feeds `${IS_DEV}`. Zero behavior change in rendered config / `--effective`. |
| `web/api/reports` `networks: [shared]` | **DROPPED** | Cannot attach an imported service to a network at the root (that IS the redefinition conflict). Pushing `networks:` into each subproject would (a) break standalone-runnability (a subproject would reference a `shared` network it doesn't define) and (b) violate "subprojects don't know about each other". No valid alternative exists for the unified-only attach under `include:`. |
| `api.depends_on.web: {condition: service_started}` | **DROPPED** | Same constraint — `depends_on` on an imported service can only be added by redefining it at the root (the conflict). Pushing it into `api/docker-compose.yml` would make `api` reference `web`, breaking standalone `api/` + the isolation invariant (TestScenario6). No valid alternative under `include:`. |
| `tools.networks: [shared]` | **KEPT** | `tools` is root-defined (not imported); attaching it is valid. |
| top-level `networks: shared` | **KEPT** | Still referenced by `tools`. |

**Blueprint-narrative impact of the two drops:** the example no longer
demonstrates (a) cross-subproject network attach of imported services from the
root, nor (b) root-only startup ordering (`api` after `web`). These were always
*invalid* under `include:` (the bug), so they were never actually working on a
strict engine — dropping them removes a latent footgun, it doesn't remove a
working feature. The `tools` service still shows the shared network + profiles
passthrough. **No test asserts on networks/depends_on/ordering** (grep below), so
this is purely a docs-narrative call for the lead.

---

## TEST IMPACT MAP

Test file: `test/cenvkit-acceptance_test.go`. Every cited line opened and read.

### Cross-cutting token grep (whole file)

- `depends_on` → **0 matches.** No test references it.
- `networks` → **0 matches.** No test references it.
- `shared` → **0 matches.** No test references it.
- `IS_DEV` → matches at lines 828–850 (Scenario17), 1380/1400 (overview, scratch
  fixture), 2089–2150 (MF4), 2173–2237 (C4 named-chain), 2461 (scratch fixture).
  Only the docker-config ones (Scenario17, MF4-parity-2) read IS_DEV from the
  monorepo's rendered compose; all PASS unchanged (value still `true`/`false`).
- `environment[0]` → **1 match, line 1063** — the ONLY assertion that breaks.

### Per failing-test verdict

| Test | file:line of key assertion | Verdict |
|---|---|---|
| `TestScenario3_CrossSubprojectPorts` | 788/792/797 (`18080`/`19090` absent, `published: "0"` present) | **PASS unchanged.** Ports still fall back to `:-0`; the imported services are untouched. Verified `published: "0"` still renders. |
| `TestScenario15_DevProdOverlay` | 815/823 (`STACK_TIER` + `dev`/`prod`) | **PASS unchanged.** Overlays still re-open the now-single-definition `web`; `STACK_TIER: dev`/`prod` still render. Verified. |
| `TestScenario17_IsDevFlag` | 841/849 (`IS_DEV` + `true`/`false`) | **PASS unchanged.** IS_DEV moved to web's own file; rendered config still shows `IS_DEV: "true"` (dev) / `"false"` (prod). Verified both tiers. |
| `TestScenario18_ProfilesCompose` | 868/871/880 (`web`+`api` present, `tools` off by default, on with profile) | **PASS unchanged.** `--services` lists `web api reports` by default, adds `tools` with `COMPOSE_PROFILES=tools`. Verified. (`reports` was already listed; test only checks web/api present + tools toggling.) |
| `TestScenario21_ReportsPort` | 898 (`15151` absent) | **PASS unchanged.** REPORTS_PORT still falls back. Verified `REPORTS_PORT: "0"`. |
| `TestV3_RunPath_Compose_L1Only` | 1298 (`18080`/`19090`/`15151` absent) | **PASS unchanged.** L1-only run path; service ports still fall back. Verified. |
| `TestValidate_Positive` | 1526 (`config valid`) | **PASS unchanged.** `validate` (CENVKIT_ENV=dev) exits 0 + prints `config valid`. Verified. |
| `TestValidate_All` | 1563/1567 (`dev config valid` + `prod config valid`) | **PASS unchanged.** `validate --all` exits 0 + both lines. Verified. |
| `TestParity_MF4_EnvEqualsEnvDebugEqualsCompose` | 2149 (`IS_DEV: "true"`), 2158 (`WEB_PORT: "0"`), 2144/2153/2162 | **PASS unchanged.** IS_DEV still chain-interpolated to `true`; WEB_PORT still falls back to `0`; SITE_URL still a chain var. Verified `IS_DEV: "true"` + `WEB_PORT: "0"` in config. |
| `TestC4_Compose_ChainNotLeaked` | 2332/2341 (`--chain ci`/`--chain=ci` config must not error `unknown flag`) | **PASS unchanged.** Restructure doesn't touch flag stripping; the `[ci]` chain still resolves and `compose config` succeeds (no conflict). Verified `compose config` exits 0 on the new root. |

### The ONE assertion that must change

**`TestProvenance_GapValue_RenderOnlyStrip`** — `test/cenvkit-acceptance_test.go:1063`:

```go
if e.Field == "environment[0]" && e.Resolved == "WEB_PORT=0" {
```

**Why it breaks:** the test asserts WEB_PORT's gap effect is at
`environment[0]`. After IS_DEV moves into web's `environment:`, compose-go orders
the environment list and **WEB_PORT lands at `environment[1]`** (IS_DEV precedes
it — verified via `--trace --var WEB_PORT --json`: `"field": "environment[1]"`).

**Required update (qa, one line):** `environment[0]` → `environment[1]`. The
rest of the assertion (`e.Resolved == "WEB_PORT=0"`, the render-only-strip
contract) is unaffected — `strip-1` (`resolves to "0"` in human) still passes.

> This is the only test edit the restructure forces. No assertion is removed; no
> other count change (`declaredAssertions = 137` stays). The `ports[0]` effect is
> unchanged.

### Other IS_DEV tests — confirmed NOT impacted

- Lines 1380/1400 (`TestEnvDebug_Overview`), 2461 (`TestProvenance` scratch) —
  use throwaway scratch fixtures (`.dev.env`/`docker-compose.yml` written inline),
  NOT the monorepo. Untouched.
- Lines 2089–2237 (MF4 #2 reads monorepo IS_DEV; C4 reads `.ci.env` IS_DEV) —
  MF4-parity-2 covered above (PASS). C4 reads IS_DEV from `.ci.env` via the chain,
  independent of where compose surfaces it (PASS).

---

## Docs to update (lead owns `docs/`)

These reference the dropped/moved bits and the `environment[0]` ordering. Listed
for the lead's narrative pass (go-engineer does NOT edit docs):

| File:line | What references the change |
|---|---|
| `examples/monorepo/docker-compose.yml` (whole `services:` block) | The file being restructured (this IS the diff). |
| `examples/monorepo/README.md:5` | "plus a shared network" — still true (tools), but no longer attaches web/api/reports; reword optional. |
| `examples/monorepo/README.md:16` | Header diagram comment: "root: include:s web/ + api/, adds the shared network" — reword: the root no longer attaches imported services to it. |
| `examples/monorepo/README.md:141–142` | `--effective` sample shows `IS_DEV=true` / `STACK_TIER=dev` — **still accurate** (both still surface on web). No change needed, but verify ordering note below. |
| `docs/guide.md:479–480` | `--effective --service web` sample shows `IS_DEV=true` + `STACK_TIER=dev` — **still accurate**; no change needed. |
| `docs/guide.md:452` | `--overview` sample `+ IS_DEV = true` under `.dev.env` — **still accurate** (IS_DEV is a chain var). No change. |
| `examples/monorepo/README.md:134–135` / `docs/guide.md:463` | gap-trace sample uses `environment[0]` implicitly (says "environment[0], ports[0]"). After the move the gap effect index is **`environment[1]`** — update the sample text if it pins the index. (README:135 says "service web environment[0]"; guide:463 says "(environment[0], ports[0])".) |

> Net docs delta is small: one ordering index (`environment[0]`→`environment[1]`)
> and one optional reword of "adds the shared network" for imported services. The
> `--effective` and `--overview` samples stay correct because IS_DEV/STACK_TIER
> still surface on `web`.

---

## Verification performed (local, v5.1.2 tolerant — structural proof is the real one)

Staged `examples/monorepo` → tmp, seeded `.env`/`.dev.env`/`.prod.env` from
`example.*`, applied the proposed root + web files, ran the freshly-built
`cenvkit`:

- `compose config` (dev): `IS_DEV: "true"`, `STACK_TIER: dev`, `WEB_PORT: "0"`,
  `API_PORT: "0"`, `REPORTS_PORT: "0"`, `published: "0"` — all as tests expect.
- `compose config` (prod): `IS_DEV: "false"`, `STACK_TIER: prod`.
- `validate` → `config valid`; `validate --all` → `dev config valid` +
  `prod config valid`.
- `compose config --services`: `api reports web`; with `COMPOSE_PROFILES=tools`:
  adds `tools`.
- `env-debug --effective --service web` (dev): still `IS_DEV=true`,
  `STACK_TIER=dev`, `WEB_DEBUG=true`, `WEB_PORT=0`.
- `env-debug --files`: two-group structure intact.
- `env-debug --trace --var WEB_PORT --json`: gap intact; WEB_PORT effect now at
  **`environment[1]`** (the single test-impacting fact).

Structural proof (version-independent): the new root redefines NO imported
service (`include:` of web/api/reports; root `services:` has only `tools`), so the
"conflicts with imported resource" error cannot arise on any engine version.

---

## Summary for lead approval

- Restructure = 2 file edits: rewrite root `docker-compose.yml` (drop the
  web/api/reports redefinitions; keep `include:`, `tools`, `networks: shared`) +
  add `IS_DEV: "${IS_DEV:-unset}"` to `web/docker-compose.yml`'s `environment:`.
- Dropped: shared-network attach on imported services + `api.depends_on.web`
  (both structurally invalid under `include:`; no valid alternative). Kept: the
  env_file→${VAR} gap demo, `include:` graph, profiles/tools, dev/prod overlays,
  STACK_TIER, IS_DEV (relocated).
- Test impact: **exactly one** assertion edit — `cenvkit-acceptance_test.go:1063`
  `environment[0]` → `environment[1]` (qa, after I freeze). All 10 named failing
  tests otherwise PASS unchanged; verified on the local engine.
- Docs: small lead-owned pass (one ordering index + one optional reword).
