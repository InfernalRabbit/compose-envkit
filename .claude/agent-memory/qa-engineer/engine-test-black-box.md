---
name: engine-test-black-box
description: engine_test.go is package engine_test (black-box); discover_test.go is package engine (white-box)
metadata:
  type: project
---

Two test files coexist in `internal/engine/`:
- `engine_test.go`: `package engine_test` — imports `github.com/InfernalRabbit/compose-envkit/internal/engine`; tests exported API (`engine.New()`, `engine.Input`, `engine.Result`).
- `discover_test.go`: `package engine` — calls unexported `resolveComposeFiles` directly.

Go allows both `package foo` and `package foo_test` files in the same directory. The two-file split is intentional: black-box tests exercise the public contract; white-box tests verify the internal gate logic.

**Why:** The comma-separator and interpolation cases in `discover_test.go` require calling `resolveComposeFiles` directly. Exporting it just for tests would widen the API surface unnecessarily.

**How to apply:** Both files compile fine in the same package directory. `go test ./internal/engine/` runs both.
