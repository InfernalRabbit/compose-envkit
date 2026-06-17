---
name: v3-acceptance-count-impl-exceeds-plan
description: cenvkit v3 acceptance count is 75 (impl added +3 prov-6) but spec D3/plan locked 72; stale "72/68/60→68" count comments survive each milestone edit
metadata:
  type: project
---

cenvkit v3 (Layer-2 debug-only) acceptance suite: header claims **75** assertions,
but spec §8 D3 + plan §5 both locked **N=72** (plan arithmetic 68−1+5=72 omitted
the +3 prov-6 inline-env invariants the impl legitimately added). D3 reserves the
final count for explicit lead sign-off at integration.

Recurring defect across cenvkit milestones (see [[s4-acceptance-count-drift]],
[[assertion-count-vs-testfunc-count]]): when assertions are added/inverted, STALE
count comments survive the edit. In v3 these were `test/cenvkit-acceptance_test.go`
lines 586 ("NOT counted in 72"), 631 ("NOT counted in 68"), 818 ("count 60→68") —
each pointing at a different historical number while the header said 75.

**Why:** the assertion count is a hand-maintained arithmetic claim, NOT derivable
from `go test` func count (v3: 36 RUN / 30 PASS funcs vs 75 claimed assertions).
Multiple section headers each carry their own count snippet, so a recount touches
the file-top header but misses the in-body section headers.

**How to apply:** on every cenvkit acceptance-count change, grep the WHOLE file for
stray numbers (`grep -nE '\b(60|68|72|75)\b.*(count|counted|assertion|net)'`), not
just the top header; flag plan-vs-impl divergence as SHOULD-FIX needing lead D3
sign-off, not a silent NIT.
