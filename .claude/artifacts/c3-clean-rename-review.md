# C3 clean-rename (`.cenvkit.envchain` / `CENVKIT_ENV`, no aliases) — code review

**Verdict: APPROVE WITH NITS** — two real but non-blocking findings (both cosmetic:
stray text in a test + a missed fixture-README). No prod-logic fix needed; the
rename itself is correct and complete in all executable paths.

Scope reviewed (frozen working tree, code + tests + fixtures; DOCS migration is the
architect's separate task, excluded):
- PROD: `internal/chain/chain.go`, `internal/engine/discover.go`, `cmd/cenvkit/main.go`,
  `internal/provenance/render.go`.
- TESTS: `chain_test.go`, `discover_test.go`, `engine_test.go`, `provenance_test.go`,
  `render_test.go`, `cmd/cenvkit/main_test.go`, `test/seam_test.go`, `test/cenvkit-acceptance_test.go`.
- FIXTURES: `examples/monorepo/` (`.docker-env-chain`→`.cenvkit.envchain`, `example*.env`,
  `docker-compose*.yml`, `web/*`).

Contract refs: C3 plan `2026-06-19-cenvkit-c3-clean-rename.md` (CLEAN BREAK, no aliases).

Full gate on this tree: `gofmt -l .` empty · `go vet ./...` clean · `go build ./...` ok ·
`go test ./internal/... ./cmd/...` all PASS · seam + stale-count guard + daemon-free
acceptance PASS.

---

## Critical
None.

## Warnings

### W1 — over-reach renamed the REAL var COMPOSE_ENV_FILES → nonexistent CENVKIT_ENV_FILES — `test/seam_test.go:10, 94, 117, 134, 140`
A blind `s/COMPOSE_ENV/CENVKIT_ENV/` caught `COMPOSE_ENV_FILES` (the real
docker-compose var that the task's #1 invariant says must NEVER be renamed),
producing 5 instances of the nonexistent `CENVKIT_ENV_FILES`. They are all in
COMMENTS (lines 10, 94) and `t.Fatal`/`t.Fatalf` MESSAGE STRINGS (lines 117, 134,
140) — the test LOGIC is untouched and correct (line 110 `envfiles.Assemble`, the
absent-path checks at 132, the `.env` presence check at 139), so the build/tests
stay green and behavior is unaffected. But the messages now name a variable that
does not exist and directly violate the "real compose var untouched" invariant.
**Fix:** revert those 5 occurrences `CENVKIT_ENV_FILES`→`COMPOSE_ENV_FILES` (the
run-path var IS COMPOSE_ENV_FILES). Owner: qa-engineer (`test/` zone). Cosmetic
severity; should still be fixed before the squashed commit so the bad token never
lands. (Note line 102 `CENVKIT_ENV=dev` IS a correct rename — that's the selector.)

### W2 — fixture README missed; still carries old names — `examples/monorepo/README.md:18, 19, 24, 26, 161, 166`
`examples/monorepo/README.md` is UNMODIFIED, but the C3 plan line 21 explicitly
lists it as a fixture edit (`README.md (:18,24,26,161,166)`), and those exact lines
plus :19 still carry `${COMPOSE_ENV}` (18, 24, 26), `.docker-env-chain` (19, 45),
and the bare `COMPOSE_ENV` selector (161, 166). This is the monorepo FIXTURE readme
(qa fixture-migration scope per the plan), distinct from the top-level user docs the
architect is migrating separately. **Fix:** apply the rename to those lines — keep
`COMPOSE_ENV_FILES` on line 45 (real var), migrate the rest. Owner: qa-engineer.
Non-blocking (a fixture doc, not executed), but it's a real completeness miss the
task's #1 focus calls out. If the lead considers this README part of the architect's
separate docs pass, reassign — but the plan put it in the fixture set.

## Suggestions

### S1 (minor) — stale-count guard is "header==const", not "count==reality" — `test/cenvkit-acceptance_test.go:63, 68`
The new `declaredAssertions = 128` const + `TestAssertionCountHeader` is a genuine
improvement and I CONFIRMED it is RED-on-drift (temp-broke const 128→129 → test
FAILED with a clear message; restored → green). It kills the recurring header/total/
prose drift class. Limitation worth noting: it only checks line-2 == const; it does
NOT verify the const equals the actual number of executed assertions, so adding
assertions without bumping either number still passes. That's acceptable for the
stated goal (it killed the 3-way drift), just not a full count-vs-reality guard.

---

## Checklist verification

1. **Completeness:** scoped grep over `internal/ cmd/ test/ examples/` — ZERO stray
   bare `COMPOSE_ENV` / `.docker-env-chain` / `${COMPOSE_ENV}` in EXECUTABLE code or
   fixtures, EXCEPT the two cosmetic misses above (W1 test messages, W2 fixture
   README). The real `COMPOSE_ENV_FILES` var is never renamed in the diff (verified:
   it appears in the diff only as untouched context / correctly-preserved real-var
   lines like main.go:299 `COMPOSE_ENV_FILES=`+envfiles.Join). render_test false-green
   FIXED — `:124` fixture (`Key: "CENVKIT_ENV"`) AND `:242/243` assertion
   (`strings.Contains(got, "CENVKIT_ENV")`) BOTH migrated.
2. **Seeding A:** chain.go:200-201 injects `CENVKIT_ENV` into the merged env; discover.go
   `interpolateComposeFile` seeds `CENVKIT_ENV` + replaces `${CENVKIT_ENV}`/`${ENV}`;
   `${ENV}` kept everywhere (chain.substituteTokens, discover, .cenvkit.envchain);
   `${COMPOSE_ENV}` dropped from all executable paths.
3. **--overview header:** render.go:219/221 now `CENVKIT_ENV = %s (from %s)` / `= %s`;
   the section TITLE `Interpolation chain (COMPOSE_ENV_FILES)` (render.go:177, 229) is
   correctly UNCHANGED (real var). render_test header assertion matches.
4. **Stale-count guard:** self-checking AND verified RED-on-drift (temp-break test, see
   S1). Header/total/prose all reconciled to 128.
5. **Fixture re-seed:** `.cenvkit.envchain` renamed + content migrated; `example.env:9`
   `CENVKIT_ENV=dev`, `:18` `COMPOSE_FILE=…docker-compose.${CENVKIT_ENV}.yml` →
   `${CENVKIT_ENV}` interpolates. `web/docker-compose.yml` `${CENVKIT_ENV:-dev}`.
6. **No C4 / logrus scope leak:** every `--chain` hit is the EXISTING `env-debug --chain`
   boolean mode (Layer-1 files view), not a C4 named-chain string selector; no
   `[section]`/`readNamedChain`/`ChainName`. logrus untouched in the C3 diff.
