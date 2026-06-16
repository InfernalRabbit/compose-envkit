---
name: macos-sed-xargs-gotcha
description: macOS BSD sed -i needs '' arg; grep -lZ | xargs -0 sed collapses filenames into one bad arg — use find -exec + per-file loop
metadata:
  type: feedback
---

On macOS (BSD toolchain), the `grep -rlZ ... | xargs -0 sed -i '' 's#..#..#g`
one-liner for a bulk import-path swap FAILS: xargs passed all the
newline/NUL-joined filenames as a single argument, so BSD sed reported
`No such file or directory` on the concatenated blob (it had already consumed
`''` as the in-place suffix and `s#..#..#g` as the script). Net effect: zero
files edited even though `go mod edit` (run earlier in the same `&&` chain)
succeeded.

**Why:** BSD `sed -i` REQUIRES an explicit backup-suffix arg (`-i ''`) unlike
GNU sed (`-i` alone). And the NUL-stream handoff to `xargs -0` didn't split into
separate argv entries the way GNU xargs does here.

**How to apply:** for bulk in-file text swaps on macOS, prefer a robust per-file
loop:
`find . -name '*.go' -exec grep -l 'OLD' {} + | while IFS= read -r f; do sed -i '' 's#OLD#NEW#g' "$f"; done`
then confirm with `grep -rn 'OLD' --include='*.go' . go.mod` (exit 1 = clean).
Scope edits to your lane with `--include='*.go'`; never `-A` / never let a swap
leak into docs/yaml owned by other agents. Related: [[compose-go-api-facts]].
