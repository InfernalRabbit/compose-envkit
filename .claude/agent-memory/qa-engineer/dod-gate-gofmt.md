---
name: dod-gate-gofmt
description: gofmt is a hard DoD gate — always run gofmt -l . before declaring done; go test alone is not enough
metadata:
  type: feedback
---

`gofmt -l .` must print NOTHING before a task is marked done. The Go test harness does NOT run gofmt — tests pass on unformatted code. CLAUDE.md lists gofmt-clean as a hard gate alongside `go vet` and `go test`.

**Why:** Lead cannot integrate (commit) a diff that fails `gofmt -l` even if all tests pass. A table-driven test struct with extra whitespace or misaligned fields passes `go test` but fails the gate.

**How to apply:** Final verify sequence is always:
```
gofmt -l . && SMOKE_SKIP_DOCKER=1 go test -count=1 ./...
```
Run `gofmt -w <file>` to fix, then re-check `gofmt -l .` prints nothing. Do this BEFORE re-freezing and reporting to lead.
