---
name: distribution-config-facts
description: cenvkit dist facts — shim/binary name collision, goreleaser v2 + golangci v2 config formats, .gitignore is lead-owned, validate YAML via in-module yaml lib when tools absent
metadata:
  type: reference
---

cenvkit distribution (T10) authored files + their gotchas:

- **Shim/binary name collision (audit W4):** the tracked POSIX shim is named
  `cenvkit` at the repo root. A bare `go build ./cmd/cenvkit` emits a binary ALSO
  named `cenvkit` → clobbers the shim. The documented vendored fast-path MUST use
  `go build -o .cenvkit.bin ./cmd/cenvkit` (dot-prefixed, namespaced). `.gitignore`
  must NOT blanket-ignore a bare `cenvkit` (would untrack the shim); ignore
  `.cenvkit.bin` + `/dist/` only.
- **`.gitignore` is LEAD-owned** (repo-config, per the module-boundary table in
  CLAUDE.md/TEAM.md) even though a task description may list it in go-engineer's
  files. Author the shim/goreleaser/CI/golangci configs yourself; hand the exact
  `.gitignore` lines to team-lead rather than editing it.
- **goreleaser config:** schema `version: 2`; `builds[].main: ./cmd/cenvkit`,
  `ldflags: -s -w -X main.version={{ .Version }}`. v2 archives use
  `formats: [tar.gz]` and `format_overrides` (NOT the old singular `format`).
- **golangci-lint config:** v2 format splits `linters:` (govet/errcheck/staticcheck/
  ineffassign/unused) from `formatters:` (gofmt). `version: "2"` at top.
- **CI seam check** = the `-deps`-FREE `go list -f '{{.ImportPath}} {{join .Imports
  " "}}' ./...` form (first-party only); grep compose-go, exclude
  $(go list -m)/internal/engine.
- **Validating YAML when goreleaser/golangci-lint/pyyaml are absent locally:** the
  module graph already has `go.yaml.in/yaml/v4` (pulled by compose-go) — a 20-line
  throwaway `go run` that `yaml.Unmarshal`s each file confirms well-formedness.
  Schema-level `goreleaser check`/`golangci-lint run` then deferred to CI.

See [[compose-go-api-facts]] for the go-doc-fallback pattern when context7 is down.
