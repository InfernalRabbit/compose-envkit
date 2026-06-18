# C4 named-chains (`[name]` sections + `--chain <name>`) — code review

**Verdict: APPROVE** — no defects, no prod fix required. One test-completeness
suggestion (non-blocking) + one count-number note.

Scope reviewed (frozen working tree):
- PROD: `internal/chain/chain.go` (INI parser + `UnknownChainError`), `cmd/cenvkit/main.go`
  (`--chain` persistent flag, `resolveChainName`, `extractPersistentFlag`, env-debug
  `--chain`→`--list`, exit-2 wiring).
- TESTS: `internal/chain/chain_test.go`, `test/cenvkit-acceptance_test.go`.
- FIXTURE: `examples/monorepo/.cenvkit.envchain` (`[ci]`/`[api]`/`[web]` sections).

Contract refs: C4 plan `2026-06-19-cenvkit-c4-named-chains.md`.

Full gate on this tree: `gofmt -l .` empty · `go vet ./...` clean · `go build ./...` ok ·
`go test ./internal/... ./cmd/...` all PASS · daemon-free C4 acceptance + guard + seam PASS.

---

## Critical
None.

## Warnings
None.

## Suggestions

### S1 (minor, test-completeness) — the `--chain` compose-leak assertion is docker-only — `test/cenvkit-acceptance_test.go:2299` (`TestC4_Compose_ChainNotLeaked`)
The security-critical "`--chain` is stripped, never forwarded to docker" assertion
is docker-gated (`t.Skip` on `SMOKE_SKIP_DOCKER=1`), so under the no-docker subset
it does not run. The strip MECHANISM (`extractPersistentFlag`) IS covered daemon-free
by `TestExtractProjectDir` (space/equals/last-wins/absent) — and `extractPersistentFlag(
args,"chain")` runs the identical code path, only the flag name differs — so the logic
is proven. But there is no daemon-free assertion exercising it with `name="chain"`
specifically. Given CLAUDE.md's own lesson ("a docker-gated assertion unrun under a
behavior change has shipped real misses"), consider a tiny `TestExtractPersistentFlag`
table (or a `_ = "chain"` case in the existing test) asserting `--chain ci` / `--chain=ci`
both strip + return "ci". Non-blocking; the docker path already covers both forms
(C4-8a/C4-8b). Owner: qa-engineer.

### Note (not a finding) — shipped count is 137, not the 136 in the assignment
The assignment said `declaredAssertions 128→136`, but the tree ships **137**: header
(line 2), `137 total` (line 41), prose (line 44), and `const declaredAssertions = 137`
(line 65) are ALL consistent at 137, and `TestAssertionCountHeader` enforces it. So the
file is internally correct — just flagging the disk value differs from the message.

---

## Checklist verification (all PASS)

(a) **Compose strip (security):** `newComposeCmd` (DisableFlagParsing) pre-scans args
   with `extractPersistentFlag(args,"chain")`, writes the value back to the shared
   persistent flag via `cmd.Flags().Set("chain",…)`, and forwards the STRIPPED args to
   docker. `extractPersistentFlag` handles BOTH `--chain VAL` (consumes next token) and
   `--chain=VAL` (prefix strip), strips ALL occurrences in any position (last-wins).
   `TestC4_Compose_ChainNotLeaked` asserts no "unknown flag" for both forms
   (C4-8a/C4-8b). `--chain` reaches `chain.Resolve` via the persistent flag, NOT docker.

(b) **env-debug `--chain`→`--list` COMPLETE:** zero `mChain`/bool-`chain` leftover
   (grep-confirmed); the persistent `--chain` is now a STRING selector (main.go:68),
   env-debug's view is `--list` BOOL (main.go:434); `HumanOpts{Chain: mList}` preserves
   the internal VIEW selector (only the cobra flag string changed). The inherited
   persistent `--chain string` threads into env-debug's `chain.Resolve` (main.go:388).
   25 `--list` test refs; all `--chain` test refs are the NEW string selector (e.g.
   `--chain ci env-debug --list`, line 2287). `--list` == bare default view (C4-6).

(c) **Scope held:** `git diff` of `internal/engine/ internal/envmap/ internal/envfiles/`
   is EMPTY — the plan's "ZERO changes" claim holds. All C4 work is `internal/chain` +
   `cmd` flags + tests/fixtures.

(d) **Orthogonality (`--chain` × `CENVKIT_ENV`):** `Input.Chain` (section select) and
   `composeEnv` (token sub) are independent fields. The `[api]` section uses `.${ENV}.env`
   and `TestNamedChain_TokensSubstituteInSection` (unit, PASS) proves `${CENVKIT_ENV}`/
   `${HOST}` still substitute WITHIN a chosen section — orthogonality proven at the unit
   layer (the precise guard; no combined CLI acceptance needed).

(e) **Guard still RED-on-drift:** TEMP-BROKE `const declaredAssertions 137→138` (header
   left at 137) → `TestAssertionCountHeader` FAILED with a clear message; restored → green.
   (First attempt used a stale baseline 136 and was a no-op; re-ran against the actual 137
   to truly confirm.) The guard genuinely fails on drift after the C4 bump.

(f) **INI parser:** header-less ⇒ implicit `[default]` (`TestNamedChain_HeaderlessIsDefault`);
   `[name]` standalone, NO inheritance (`TestNamedChain_StandaloneNoInheritance` — verifies
   `[api]` excludes default files); missing section → `*UnknownChainError` with sorted names
   excluding "default" (`TestNamedChain_MissingSectionError`); `default` valid both ways;
   empty default → exit 0. BOTH edge cases I pre-flagged are tested:
   `TestNamedChain_ExplicitDefaultHeaderConcatenates` and `TestNamedChain_EmptyBracketKey`.
   `main()` maps `*chain.UnknownChainError` → exit 2 (distinct from generic exit 1).

**No C3/logrus/docs-sync scope leak:** logrus untouched; no README/AGENTS edits in the
diff (CHANGELOG is the architect's expected breaking-change note).
