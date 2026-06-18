# C2 populator (`run` / `env` / `Flatten` / U1) — code review

**Verdict: APPROVE** — one non-blocking doc finding (stale assertion count, qa zone)
+ two optional suggestions. No prod fix required; the contract is correct.

Scope reviewed (frozen working tree, pre-integration):
- PROD: `internal/engine/flatten.go` (NEW), `internal/engine/provenance.go` (MOD, U1),
  `internal/envmap/envmap.go` + `emit.go` (NEW), `cmd/cenvkit/main.go` (MOD: run/env).
- TESTS: `internal/engine/flatten_test.go` (NEW), `internal/envmap/envmap_test.go` (NEW),
  `cmd/cenvkit/main_test.go` (MOD), `test/cenvkit-acceptance_test.go` (MOD).

Contract refs: spec §5; plan `2026-06-19-cenvkit-c2-populator.md`.

Full gate on this tree: `gofmt -l .` empty · `go vet ./...` clean · `go build ./...` ok ·
`go test ./internal/... ./cmd/...` all PASS · daemon-free acceptance subset PASS.
(Docker acceptance is the lead's frozen-tree gate — reported 69 PASS / 0 SKIP / 0 FAIL.)

---

## Critical
None.

## Warnings
None.

## Suggestions

### S1 (minor, doc — qa zone) — three inconsistent assertion-count totals — `test/cenvkit-acceptance_test.go:2, 39, 42`
The C2 bump produced a THREE-way mismatch, and the bump itself is wrong arithmetic:
- line 2 `Current assertion count: 133`
- line 39 `133 total`
- line 42 `included in the 131 count`

The batch lines (27-37) actually sum to **128**:
`68 −1 +5 +3 +3 +1 +28 +1 +3 +4 +13 = 128` (re-summed via `bc`), which also equals
the committed C1 baseline (115, verified `git show HEAD:…`) + this cycle's `+13 C2`
line. So all three printed numbers are wrong; the correct total is **128**. Fix: set
lines 2, 39, and 42 all to 128. Comment-only, zero behavior impact — fine to fold
into the squashed C2 commit. (Recurring class on this suite; see C1 review S1.)

### S2 (optional, run UX) — `run --print` still requires `--` — `cmd/cenvkit/main.go` newRunCmd RunE
`--print` is checked AFTER the `ArgsLenAtDash()<0` and `len(args)==0` guards, so
`cenvkit run --print` (no `--`, no command) exits 2 rather than printing the env.
Spec §5d says `--print` "skip exec, always exit 0" but does not explicitly exempt
the `--` requirement, so this is defensible (and the acceptance test deliberately
uses `run --print -- false`). Flagging only as a UX call for the lead: if `--print`
is meant as an inspect-without-a-command affordance, it would naturally run without
`--`. No change needed if the `-- <cmd>` shape is intentional for `--print` too.

### S3 (optional, nit) — `signal.Notify(sigCh)` forwards ALL signals — `cmd/cenvkit/main.go` execChild
`signal.Notify(sigCh)` with no signal list subscribes to every signal, so the
forwarding goroutine relays `SIGURG` (Go runtime async-preemption) and `SIGCHLD` to
the child too. Harmless in practice (children ignore/expect these), and the
128+signo mapping reads the CHILD's own wait-status, not the forwarded set, so exit
fidelity is unaffected. If you want to be tidy you could scope to
`os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT`. Non-blocking.

---

## Checklist verification (all PASS)

1. **Seam:** `go list -f '{{.Imports}}' ./internal/envmap/` = `[encoding/json fmt
   <engine> io sort strings]` — pure-Go, imports `internal/engine`, NO compose-go.
   Only `internal/engine` imports compose-go directly (verified via direct-imports
   grep, NOT `-deps`). `flatten.go` adds only `dotenv` (already an engine dep).
2. **U1 correctness (moat-critical):** `interpEnv` now built via `engine.Flatten(
   shellMap, layer1Files, true)` then shell overlaid last (provenance.go ~:186-199);
   `chainEnv` stays the incremental dotenv-lookup map (split PRESERVED, not
   collapsed). Critically, the gap-detection block (lines 428-448) is **byte-
   unchanged** vs C1: `_, vt.InChain = interpEnv[k]` and `vt.Gap = referenced &&
   !vt.InChain && len(vt.RuntimeDefs)>0`. The U1 swap changes how interpEnv VALUES
   resolve (full-base parity) but not its KEY SET (still every Layer-1 file key), so
   `InChain` membership and thus `Gap` are unaffected. Engine tests + docker
   acceptance green → zero golden drift confirmed (D2 probe holds on disk). The MF4
   parity acceptance (`TestParity_MF4_*`) genuinely pins `env`/`compose config`/
   `env-debug --effective` agreement on chain var (SITE_URL), interpolation feed
   (IS_DEV→"true"), and the gap-var asymmetry (WEB_PORT absent from env, "0" in
   config, present in --effective).
3. **shell-wins:** `envmap.Resolve` clones processEnv then adds a chain key only
   `if _, set := full[k]; !set` (add-only-unset, mirrors `cli.WithDotEnv` Merge).
   `Full` (run execs) vs sorted `ChainKeys` (env emits only these) split is correct;
   Emit pulls values from `Full[k]` so a shell override of a chain key is reflected.
   Applies identically on `--expand`/`--no-expand` (overlay is outside the expand
   branch). `mapToEnvSlice` sorts for determinism.
4. **run exit mapping:** `--`/empty enforced via `ArgsLenAtDash()<0`→2 and
   `len(args)==0`→2; `execChild` pre-`LookPath`s to split 127 (not found) / 126
   (perm) BEFORE Start; child non-zero→`ExitCode()`; signal-terminated→`128+signo`
   via `WaitStatus.Signaled()`. Reuses C1's `exitError` seam. Unit tests pin 127 /
   126 / 128+15(SIGTERM, white-box) / child-code-3 / exit-2-both-cases.
5. **env --format quoting:** `shell` = single-quote + `'\''` idiom + reject
   non-identifier keys (`validShellIdent` correctly bars leading-digit and `.`/`-`);
   `dotenv` double-quotes and escapes `\ " $ \n` to round-trip compose-go's parser;
   `json` stdlib. `--no-expand` routes through `engine.ParseOrderedLiteral` (no third
   literal reader — the T1 rename). envmap_test covers space/newline/quote/`$`.
6. **Scope:** diff + new-file grep for `cenvkit.envchain|CENVKIT_ENV|--chain|named-
   chain|mask|redact` → nothing (the lone "secrets" hit is the `--print` "reveals
   plaintext secrets by design" doc comment — masking is correctly OUT of scope). No
   C3 rename, no C4 named-chain.
7. **Test guard-validity:** flatten_test pins missing-file-errors on BOTH paths (MF2
   invariant), base-not-in-result, unset-var→empty; run exit codes are real
   subprocess/white-box assertions over new behavior (fail by construction if wrong).

Note: the logrus "variable not set" stderr line is the accepted/deferred known
limitation (compose-parity, stdout unchanged) — not reviewed as a finding per the
lead's instruction.
