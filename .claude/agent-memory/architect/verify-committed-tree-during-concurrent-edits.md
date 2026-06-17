---
name: verify-committed-tree-during-concurrent-edits
description: When committing teammate work during concurrent edits, verify the COMMITTED/staged subset (not just the working tree) — a green working-tree test can hide an inconsistent staged commit
metadata:
  type: feedback
---

Before declaring a commit green, verify the **committed tree** (the exact staged
subset), not just the working tree — especially when teammates are concurrently
editing related files. `go test`/build run against the WORKING TREE; if you
`git add` only a subset while the tree is mid-flux, the working-tree test can pass
while the **staged/committed subset is internally inconsistent**.

**Why:** 2026-06-16, v2 P5. I committed `3e171de` with `provenance/model.go`
(added `Report.ChainFiles`) + `render.go` (`--chain` reads `r.ChainFiles`), and my
`SMOKE_SKIP_DOCKER=1 go test ./...` passed — but that pass was against a
working-tree state that still had the *interim* wiring. The engine change that
*populates* `rep.ChainFiles` was never staged (go-engineer was concurrently
reworking the fix from HumanOpts.ChainFiles → Report.ChainFiles). Result: HEAD
shipped broken — `env-debug --chain` printed `[]`, acceptance `[12.4]` failed on a
clean checkout. The three-part fix (model + render + engine-populate) spanned
files owned/edited by different agents in the same window, and I staged only two.

**How to apply:**
- When committing during multi-agent flux, after `git add <subset>` run the suite
  against the **committed state**: `git stash -u && go test ./... && git stash pop`
  (or `git worktree`/checkout the commit). If it fails, the staged subset is
  incomplete — find the missing file.
- For a fix that spans >1 file/owner (here: model+render+engine for one field),
  treat it as ONE unit — confirm ALL parts are staged before committing; don't
  commit it piecemeal across turns while edits are still landing.
- Prefer freezing teammates on the exact set before an expensive verify (the
  TEAM.md "freeze on the reported hash" rule applies to working-tree commits too,
  not just reported hashes). See [[thin-engine-compose-owns-resolution]] context.
- **Freeze ALL teammates, not just one, before running "final" verification.**
  2026-06-17 (v3 Layer-2-debug-only): I ran `go test ./...` with go-engineer frozen
  but qa still mid-edit on the package unit tests. The run "failed" with line
  numbers + assertion messages that did NOT match the file on disk (it reported
  `provenance_test.go:78 "WEB_PORT effect wrong: [...]"` while disk line 81 already
  read the updated v3 message) — a dead giveaway the tree moved under me. A raced
  verify yields stale, misleading results; don't trust or act on them. Gate: every
  teammate touching the tree reports "frozen + my-suite green" BEFORE you verify.
  Bonus signal it caught (legitimately): test scope was broader than the handoff
  named — package-level `*_test.go` (internal/**) needed the v3 updates too, not
  just the `test/` acceptance suite. When a behavior contract changes, sweep ALL
  `*_test.go`, and make the gate a full `go test ./...`, not just the acceptance dir.
