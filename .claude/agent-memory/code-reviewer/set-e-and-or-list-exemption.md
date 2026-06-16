---
name: set-e-and-or-list-exemption
description: "set -e does NOT abort on a failing command that is the operand of && or || — recurring false-positive in seam-check / one-liner reviews"
metadata:
  type: project
---

A `cmd && a || b` one-liner does NOT abort under `set -e` when `cmd` fails.
POSIX rule: a command whose exit status is tested by `&&`/`||` (i.e. it is not
the last command of an AND-OR list) is EXEMPT from `set -e`. So in the cenvkit
seam check

```
go list -f ... | grep -v "^$MOD/internal/engine " | grep 'compose-spec/compose-go' && echo LEAK || echo "seam OK"
```

the failing final `grep` (no-leak case) is the left operand of `&& echo LEAK`,
its non-zero exit is consumed, "seam OK" prints, pipeline exits 0. Verified live
in both `bash -ec` and POSIX `sh -ec` (dash). A "this aborts under set -e"
finding against this construct is real=false.

Separate, genuine weakness (NOT what such findings usually claim): on a REAL
leak the same one-liner prints "LEAK" but exits 0 (echo succeeds), so it does not
fail a build by exit code. That's fine for the cenvkit plan because Task 2 Step 6
is a LOCAL human/agent verification step ("Expected: seam OK"), not the gate. The
authoritative gate is .github/workflows/ci.yml (lines 45-53), which uses the
anchored form + `|| true` + `[ -n "$leaks" ]` + `exit 1` and is correct.

**Why:** reviewers keep mis-citing `set -e` aborts on grep in `&&`/`||` chains;
test the actual shell before accepting. Related: [[seam-check-go-list-deps-false-positive]].

**How to apply:** any finding asserting "X aborts under set -e" where X is the
operand of `&&`/`||` (or in a pipeline tested by them) — reproduce with
`bash -ec` AND `sh -ec` before scoring it real.
