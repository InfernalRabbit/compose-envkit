---
name: include-conflict-vs-overlay-merge
description: compose include: refuses to merge an imported service with a same-named root services: entry (errors on strict engines); -f overlay merge is different and OK. Adding an env key shifts compose-go environment[N] indices.
metadata:
  type: project
---

In `examples/monorepo`, the root `docker-compose.yml` both `include:`d the
web/api/reports subprojects AND redefined those same services under root
`services:` to add `networks`/`depends_on`/`IS_DEV`. Docker Compose **does not
merge an `include:`-imported service with a same-named main-file service — it
errors** (`services.web conflicts with imported resource`; docker/compose #11488,
#11404). This is by-design and version-dependent in *tolerance*: local engine
v5.1.2 silently merges (exit 0), CI v2.38.2 errors (red). The fix is structural:
the root must redefine NO imported service.

**Why this matters / how to apply:**
- **`include:` conflict ≠ `-f` overlay merge.** `docker-compose.dev.yml` /
  `.prod.yml` re-open `web` to add `STACK_TIER` and this is FINE — they are `-f`
  overlay layers (COMPOSE_FILE list), which merge into the single web definition.
  Only an `include:`-imported service redefined in the SAME file's `services:`
  conflicts. Don't conflate the two when restructuring.
- Cross-cutting bits valid only on an imported service from the root
  (network attach, `depends_on` across subprojects) have NO valid home under
  `include:`: pushing them into the subproject breaks standalone-runnability +
  subproject isolation (TestScenario6). Drop them; keep root-OWNED services
  (e.g. `tools`) attaching to the shared network.
- A subproject-local env key (e.g. `IS_DEV: "${IS_DEV:-unset}"`) belongs in that
  subproject's own `environment:` — still chain-interpolated, zero rendered-config
  change. But: **adding an environment key shifts compose-go's `environment[N]`
  effect indices.** Adding IS_DEV before WEB_PORT moved WEB_PORT's gap effect from
  `environment[0]` to `environment[1]` (compose-go orders the env list;
  IS_DEV < WEB_PORT). Acceptance test `cenvkit-acceptance_test.go` pins
  `e.Field == "environment[0]"` — any env-key addition there is a test-impacting
  change to flag for qa. Verify the new index with
  `env-debug --trace --var <V> --json` before claiming.
See [[monorepo-fixture-layer1-needs-seeding]].
