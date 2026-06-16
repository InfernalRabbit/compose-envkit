---
name: seam-check-echo-leak-exits-zero
description: seam/guard shell `... && echo LEAK || echo OK` prints LEAK but exits 0 — a CI gate that cannot fail; require exit 1 in the leak branch
metadata:
  type: project
---

The cenvkit provenance plan (Task 2 Step 6, plan line 584) declares the
compose-go import seam check as a **CI-enforced** contract (plan line 22:
"CI seam check enforces it"), but the command

```
... | grep 'compose-spec/compose-go' && echo LEAK || echo "seam OK"
```

prints `LEAK` on a real leak yet **exits 0** — even under `set -e` (bash -ec and
sh -ec both verified: script-exit=0 on the leak case, because `echo LEAK` is the
last successful command of the AND-OR list). So a real seam violation leaves CI
green. Guard-that-cannot-fail / green-from-birth defect class.

Fix (portable, verified bash + POSIX sh): make the leak branch fail —
`... && { echo LEAK; exit 1; } || echo "seam OK"`. The `exit 1` terminates before
`||` is reached → leak=exit1, clean=exit0.

**Why:** ties to [[seam-check-go-list-deps-false-positive]] (the `-deps`-free,
$MOD-restricted *content* of this same check was already fixed; the exit-code
*tail* is a separate defect) and [[carried-bug-classes-cenvkit]] (guards must be
RED on the bad state).

**How to apply:** any review of a shell guard/seam/leak check — verify the
FAILURE branch actually returns non-zero. `echo MESSAGE` is not a failure. Test
with `bash -ec` AND `sh -ec` and read `$?` of the whole construct, not just that
the message printed. Distinct from [[set-e-and-or-list-exemption]]: there the
guard was a false positive (grep as &&/|| operand doesn't abort); here the guard
is a false negative (it never aborts even when it should).
