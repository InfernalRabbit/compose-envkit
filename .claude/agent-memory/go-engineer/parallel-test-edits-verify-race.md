---
name: parallel-test-edits-verify-race
description: When qa edits tests in parallel, a failing test may be mid-flight — re-read the CURRENT test source (uncached) before designing a prod fix, or you'll fix against a stale assertion
metadata:
  type: feedback
---

When qa-engineer authors/inverts tests in parallel in the SAME working tree, a
red test you see may be mid-edit. In the v3 round, `TestProvenance_BLite_And_C`
changed THREE times across my reads; I designed a prod fix (seed a VarTrace for
every service-env_file var so unreferenced Layer-2-only vars carry RuntimeDefs)
against one snapshot, but qa's ACTUAL contract was the opposite — an unreferenced
Layer-2-only var must be ABSENT from `rep.Vars` (its value is reachable only via
C/`--effective`). I had to revert the seeding (net-zero prod change).

**Why:** in a shared tree with a parallel test author, `go test` output and the
test source can race. Acting on a stale assertion wastes a fix+revert cycle and
risks shipping prod logic that contradicts the real contract.

**How to apply:** before writing prod code to satisfy a failing test:
1. Re-Read the CURRENT test source (the assertion + its comment) — comments often
   state the intended contract explicitly (e.g. "WEB_ONLY must NOT appear in
   rep.Vars under v3").
2. Run the specific test UNCACHED (`go test -run X -count=1`) so the failure
   matches the source you just read.
3. If the contract is genuinely a prod bug, fix; if it's an inverted/stale
   assertion in qa's zone, DM qa — never edit `*_test.go` yourself.
4. Distinguish a real prod bug from a stale fixture: a fixture asserting an
   IMPOSSIBLE engine state (e.g. a `layer2` Winner with InChain unset — v3 never
   produces a Layer-2 chain winner) is the fixture's bug, not the renderer's.

See [[v3-a-attribution-two-env-trap]] and [[v3-layer2-debug-only-gap-detector]].
The verify-before-claim rule (cite a file:line you actually opened) extends to
test files that another agent is concurrently editing.
