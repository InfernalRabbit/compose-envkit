---
name: seam-check-go-list-deps-false-positive
description: the cenvkit "only internal/engine imports compose-go" CI gate uses `go list -deps` which makes it a PERMANENT false positive — drop -deps
metadata:
  type: project
---

The plan's engine-seam CI gate (Task 3 Step 8) is:
```
go list -deps -f '{{.ImportPath}} {{join .Imports " "}}' ./... \
  | grep compose-spec/compose-go | grep -v '/internal/engine' && fail || ok
```
This is a PERMANENT FALSE POSITIVE. `-deps` expands to every TRANSITIVE package,
so compose-go's own ~17 sub-packages (`.../v2/types`, `.../v2/loader`,
`.../v2/cli`, ...) appear in the left column. Their import paths all match
`compose-spec/compose-go` and none contain `/internal/engine`, so the gate fires
"compose-go leaked" on EVERY clean build. Live-verified against v2.11.0.

Correct form (drop `-deps`; first-party packages only):
```
MOD=$(go list -m)
go list -f '{{.ImportPath}} {{join .Imports " "}}' ./... \
  | grep -v "$MOD/internal/engine" | grep "compose-spec/compose-go" && fail || ok
```
Verified: corrected form is GREEN when clean and RED when a real compose-go
import is injected into internal/chain.

**Why:** `-deps` means "include dependencies"; a "which of OUR packages imports
lib Y" gate must restrict to the module's own packages. A guard that is RED from
birth (here, always-red) proves nothing and trains people to ignore it.

**How to apply:** any "only package X may import library Y" gate — never `-deps`;
restrict the left column to `$(go list -m)`. Same family as the guard-validity
rule in [[carried-bug-classes-cenvkit]] (a guard must distinguish pass from fail).
Related: [[compose-go-option-order-and-compose-file]].
