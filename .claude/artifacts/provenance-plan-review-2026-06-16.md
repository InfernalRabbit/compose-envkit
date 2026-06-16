# Review — cenvkit v2 rich-provenance PLAN (provenance mechanism correctness)

Plan: `docs/superpowers/plans/2026-06-16-cenvkit-rich-provenance.md`
Spec: `docs/superpowers/specs/2026-06-16-cenvkit-rich-provenance-design.md`
Probe: `.claude/artifacts/compose-go-provenance-probe.md`
All compose-go v2.11.0 API signatures in the plan were re-verified with `go doc`
against the pinned module — they are correct (`LoadConfigFiles`,
`LoadModelWithContext`, `loader.Options`, `cli.WithoutEnvironmentResolution`,
`template.ExtractVariables/SubstituteWithOptions/WithoutLogging/Mapping/DefaultPattern`,
`dotenv.ReadFile`, `types.Mapping`==`map[string]string`, `types.ConfigDetails.Environment`,
`ServiceConfig.EnvFiles`/`Environment`==`MappingWithEquals`==`map[string]*string`).

## Critical

### C1 — B-lite (and C inline) interpolation mapping is seeded from Layer-1 only; misses Layer-2 env_file vars that ARE the product
Plan T2 Step 3, `internal/engine/provenance.go` lines 385-386 (`chainEnv := envSliceToMap(in.Env)` / `mapping`), used at line 493 (`SubstituteWithOptions(leaf, mapping, ...)`) and 474 (`details.Environment`), plus the C load `cli.WithEnv(in.Env)`+`WithInterpolation(true)` at lines 423-424. `in.Env` is fed `cr.Vars` (cmd T3 line 638), which is chain Layer-1 only (`internal/chain/chain.go:24-25,189-208`). The monorepo's `WEB_PORT=18080` lives ONLY in `web/.web.env`, a Layer-2 service env_file (`examples/monorepo/web/.web.env`, `web/docker-compose.yml` `${WEB_PORT:-0}:80`). chain.Resolve never reads service env_files, so `WEB_PORT` is absent from `cr.Vars`. Therefore `SubstituteWithOptions("${WEB_PORT:-0}:80", mapping)` resolves to `"0:80"`, not `"18080:80"`. Plan T3 Step 3 smoke and T4 Step 1 acceptance assert the real resolved port → they FAIL. The probe passed only because it injected `WEB_PORT=8080` into the env slice directly; production sources it Layer-2. Same root cause breaks C's inline `WEB_PORT: "${WEB_PORT:-0}"` value (resolves to "0").
**Fix:** build the interpolation env from the MERGED COMPOSE_ENV_FILES (Layer-1 ∪ Layer-2, last-wins), not `in.Env`. In `Provenance`, after the A loop, fold every `in.EnvFiles` parse into `chainEnv` in order, THEN overlay `in.Env` (OS-wins), and use that merged map as `template.Mapping`, `details.Environment`, and the `cli.WithEnv` slice. This mirrors how `docker compose` reads COMPOSE_ENV_FILES before interpolation. The plan's own engine unit test (T2 Step 1) masks this because it pre-seeds `WEB_PORT` into `in.Env` — fix the test to source it from the env_file too, so it's RED on the broken code.

## Warnings

### W1 — `--value` regresses from Layer-1-only to full-chain (Layer-2 leak / secret-scope)
Plan T1 `RenderHuman` (lines 186-187): `case o.Value != "": fmt.Fprintln(w, r.Vars[o.Value].Value)`. `r.Vars` is A-attribution over `in.EnvFiles` = Layer-1 + Layer-2 (cmd T3 lines 619-633). v1's `--value` is **Layer-1 ONLY** (`internal/debug` `Value(cr.Files, ...)`, smoke.sh:218 "--value sources ONLY the project chain"). A Layer-2-only var (or a Layer-2 secrets file value) queried via `--value` now returns a value v1 would not — a silent secret-scope leak. The SMOKE_VAL acceptance won't catch it (SMOKE_VAL is in `.env`, Layer-1).
**Fix:** scope `--value` to Layer-1. Either carry `Layer` on `VarTrace`/`Source` and have render filter to layer1 winners for `--value`, or have cmd pass a Layer-1-only file list when serving `--value`. Add a Layer-2-only var to the acceptance and assert `--value` returns empty for it (RED on current plan).

### W2 — A-attribution winner Value ignores the OS-env overlay (winner Value can disagree with B-lite resolved)
Plan T2 Step 3 lines 398-409: `Value` is taken from raw `parseDotEnv(f.Path)`. chain applies OS-env last-wins over file vars (`chain.go:189-196`); `in.Env`/B-lite mapping use the OS-overlaid values. So if a var is set in a chain file AND exported in the shell, `--trace` reports the file value as the winner Value while the B-lite effect resolves with the OS value — internally inconsistent output.
**Fix:** after computing the A winner file, set the reported `Value` from the merged env (OS-overlaid) for that key, or document that the shell overlay is a synthetic "environment" source that wins over all files (add it as a final Source in `in.EnvFiles` order with `Layer:"environment"`).

## Suggestions / Notes (non-blocking)

### N1 — seam-check `&&...||` does not fail CI on a real leak
Plan T2 Step 6: `... | grep 'compose-spec/compose-go' && echo LEAK || echo "seam OK"`. The corrected no-`-deps`, `$MOD`-restricted form is right (good — prior lesson applied), but `echo LEAK` exits 0, so CI stays green on an actual leak. Make the leak branch `&& { echo LEAK; exit 1; }`.

### N2 — top-level `${VAR}` refs (networks/volumes/configs/secrets/x-*) are dropped (documented scope, not a bug)
`splitServiceField` (plan lines 556-567) returns ok=false for any non-`services.` path; walkDict callback early-returns (lines 489-491). A var used only in a top-level block yields empty Effects. Defensible B-lite narrowing — state it in spec §2/§8 and a code comment, don't widen silently.

### N3 — plan↔spec architecture drift (tracked, lead's call)
Plan implements `engine.Provenance -> provenance.Report` directly; spec §4/§5 mandate `engine.ProvenanceFacts` + `provenance.Build` + chain-A via `engine.ParseDotEnv`. Plan's self-review claims §4/§5 covered ✓ — false by type names. Either update the spec to match the (cleaner) plan or restore the spec types. Not a correctness break.
