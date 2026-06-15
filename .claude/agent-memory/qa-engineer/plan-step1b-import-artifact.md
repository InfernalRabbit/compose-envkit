---
name: plan-step1b-import-artifact
description: Plan Step 1b shows `import "bytes"` inside an append code block — this is a plan formatting artifact, not valid Go
metadata:
  type: feedback
---

The implementation plan's Task 6 Step 1b has `import "bytes"` embedded inside an `// append` code snippet. In the actual Go test file this import must go in the file-level import block alongside `os`, `path/filepath`, and `testing`. Placing it anywhere else is a compile error.

**Why:** The plan is written as incremental append snippets for documentation clarity, not as a literal copy-paste target. Imports are always file-level in Go.

**How to apply:** When the plan shows an `import "X"` inside an append block, fold it into the file's existing import declaration.
