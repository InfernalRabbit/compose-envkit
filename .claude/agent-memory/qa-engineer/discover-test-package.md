---
name: discover-test-package
description: discover_test.go must be package engine (white-box) to call unexported resolveComposeFiles
metadata:
  type: project
---

`internal/engine/discover_test.go` uses `package engine` (NOT `engine_test`) because `TestHasComposeFile` calls the unexported `resolveComposeFiles(dir string, env []string) []string` directly. This is a deliberate white-box test — the plan spec says "table tests for `HasComposeFile`" but the actual assertions call `resolveComposeFiles` to verify the comma-guard and interpolation cases.

**Why:** `HasComposeFile` is a thin wrapper; the discriminating cases (comma-as-non-separator, interpolation before stat) are only testable by calling `resolveComposeFiles` directly.

**How to apply:** Keep `discover_test.go` in `package engine`. Do not merge it with `engine_test.go` (which is `package engine_test`).
