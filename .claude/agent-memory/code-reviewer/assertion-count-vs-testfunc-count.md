---
name: assertion-count-vs-testfunc-count
description: cenvkit acceptance header counts ASSERTIONS (60), not Test funcs (30); don't treat func-count != assertion-count as a drift defect
metadata:
  type: project
---

`test/cenvkit-acceptance_test.go:1-17` header says "60 assertions" but the file
has ~30 `func Test*`. These are DIFFERENT units — one Test func emits multiple
assertions (e.g. Scenario 8 = "8.1 ... AND .api.env"; Scenario 3 = "3 assertions").

**Why:** A coverage-consistency reviewer flagged "header says 60 but 30 funcs" as
a discrepancy proving count drift. It is NOT — 30 funcs producing 60 assertions
is internally consistent.

**How to apply:** When a finding compares the "60 assertions" header to the
`grep -c "^func Test"` number, that specific sub-claim is false. The real count
issue is separate: the v2 plan leaves the NEW total as an unfilled TODO (see
[[s4-acceptance-count-drift]]) — THAT is the actionable defect, not the
func-vs-assertion arithmetic.

Also verified: NO Go acceptance test uses `--trace`/`--diff`/`debug.Trace`. The
only `--trace` assertion is legacy `smoke.sh:213` (`--trace --var SVC_PORT`) and
it is EXIT-0-ONLY (`run_edbg` never inspects output format) and was NOT ported to
Go. So a v2 plan that changes `--trace` output semantics breaks no existing
acceptance assertion — claims that "existing v1 --trace file-list assertions will
break" are false.
