---
name: blank-import-reference-not-a-lint-failure
description: `_ = pkg.Fn` to keep an import alive is NOT flagged by the repo's enabled golangci linters; "this trips golangci-lint" findings are real=false unless probed
metadata:
  type: project
---

A `_ = sort.Strings` style blank reference (used to keep an otherwise-unused
import compiling) does NOT fail this repo's lint gate.

**Why:** cenvkit `.golangci.yml` (v2 format) enables exactly: errcheck, govet,
ineffassign, staticcheck, unused + gofmt formatter. Empirically probed
(2026-06-16) against the exact `_ = sort.Strings` pattern:
- go build / go vet / gofmt: clean (the blank ref "uses" the import).
- staticcheck default checks: exit 0 (U1000/unused does NOT flag blank refs to
  imported funcs; only flags unused *declared* members).
- ineffassign: exit 0 (blank `_ =` assignment is the idiomatic suppression it
  ignores by design).
- errcheck: only flags `fmt.Fprintln` unchecked-error (default-excluded for
  fmt.*; v1 render code already uses this pattern and passes CI), NOT the sort ref.

**How to apply:** When a reviewer claims a blank import-keeper "trips golangci-lint"
or "staticcheck/unused may flag it", treat as real=false unless they probed the
actual enabled linter set. It IS a mild code smell (dead import kept artificially)
and removing it is a fine cleanup, but it is NOT a build/CI breaker — so the
severity is over-stated. Related: [[seam-check-go-list-deps-false-positive]],
[[set-e-and-or-list-exemption]] (other "looks-broken-but-isn't" CI gate cases).
