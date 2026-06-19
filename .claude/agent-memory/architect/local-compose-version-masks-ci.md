---
name: local-compose-version-masks-ci
description: Local TOOL versions (docker compose, golangci-lint) differ from CI's — a green local run hides CI failures from tool-version mismatch (compose tolerance; golangci-lint build-Go). Test against CI's versions, not just local.
metadata:
  type: feedback
---

**The local `docker compose` the acceptance gate runs against is NOT the same
version CI runs — and a green local acceptance is necessary, not sufficient.**

**Why:** 2026-06-16 → 2026-06-18 CI's docker acceptance was RED on **every push**
for ~3 days (first red `a50045d3`; the C1–C4 merge `abc0127` landed on an
already-red master — it did NOT introduce it). Real error (masked in the test as
a bare "exit status 1"): **`services.web conflicts with imported resource`**. The
example monorepo root `docker-compose.yml` `include:`s the subproject files (which
define `web`/`api`/`reports`) and ALSO redefines those same services under
`services:` to add cross-cutting wiring (shared network, depends_on, IS_DEV).
Strict Compose forbids overriding an `include:`-imported service in the same file.
**Local docker compose was v5.1.2 (tolerates/merges it); CI ubuntu-latest is
v2.38.2 (hard error).** So local `go test ./test/...` passed for days while CI was
red, across the entire C1–C4 build.

**How to apply:**
- When the gate touches `docker compose`, local-green ≠ CI-green. Verify against
  CI's version or cenvkit's documented compose floor — pin/probe it, don't trust
  whatever Docker Desktop happens to ship.
- **Surface subprocess stderr.** The acceptance helper printed only the wrapped Go
  error ("exit status 1"), hiding docker's real message — turning a 1-line fix into
  a log-spelunking session (had to pull the GitHub Actions log via the git
  credential-helper token since `gh` isn't installed + the API logs endpoint 403s
  unauthenticated). Print `CombinedOutput()` on failure.
- **`dockerAvailable()` (test/cenvkit-acceptance_test.go) only checks
  `SMOKE_SKIP_DOCKER != "1"`, not real docker presence** — so `go test ./...` on a
  no-docker host (macOS CI runner) RUNS the docker-gated tests and they fail. It
  must also probe `docker compose version` succeeds.
- **golangci-lint isn't installed locally**, so CLAUDE.md's "golangci-lint if
  installed" gate silently skipped it — CI caught 15 `errcheck` issues in the new
  C1–C4 code. Install + run it in the local gate, or stop calling the gate "full".
- **golangci-lint's CI binary must be built with the project's Go.** the
  `golangci-lint-action` ships a prebuilt binary built with an OLDER Go (go1.24)
  that refuses to lint a go1.26 go.mod (`the Go language version used to build
  golangci-lint is lower than the targeted Go version`) — RED on the first v0.5.0
  push (everything else green). Fix: `go install
  .../golangci-lint/v2/cmd/golangci-lint@<ver>` in the lint job so it's built with
  the job's Go (not the action's prebuilt binary).
- **golangci-lint UNDERCOUNTS its printed issues** (`max-same-issues: 3` /
  `max-issues-per-linter: 50` defaults): an "11 issues" view was really ~40
  errcheck sites. Fix to a true `0`; don't treat the printed list as exhaustive.

See also [[cenvkit-c1-c4-build-orchestration]], [[verify-committed-tree-during-concurrent-edits]].
