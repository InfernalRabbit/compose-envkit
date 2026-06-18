---
name: stale-count-comment-recurs-per-cycle
description: Acceptance header assertion-count bumps leave a stale duplicate count comment elsewhere in the file EVERY cycle; grep the WHOLE file
metadata:
  type: feedback
---

When a cycle adds acceptance assertions, the author bumps the header `Current
assertion count: N` (test/cenvkit-acceptance_test.go line 2) and the `N total`
batch line, but leaves OTHER count comments stale. C1 bumped 111→115 at lines
2/38 but left line 41 ("included in the 111 count") stale.

**Why:** this is a *recurring* defect class on this suite — see
[[v3-acceptance-count-impl-exceeds-plan]] (stale 72/68/60→68 at lines 586/631/818)
and [[s4-acceptance-count-drift]]. The header count and the prose count comments
live far apart in one big file; a single bump misses the duplicates.

**How to apply:** on ANY acceptance change that touches the count, grep the WHOLE
file for every integer that could be a count, not just the header:
`grep -nE '[0-9]+ (total|assertion|count)|count: [0-9]+' test/cenvkit-acceptance_test.go`
— then reconcile EACH hit against the new total. Watch out for `0o111`/`0o644`
file-mode false positives (octal literals, not counts). This is a minor/doc-nit
finding (non-blocking, comment-only), but flag it every time — it compounds.
