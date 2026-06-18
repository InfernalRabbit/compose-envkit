---
name: stale-count-guard-pattern
description: The const+test pattern that permanently kills the recurring assertion-count drift class (4 occurrences by 2026-06-19)
metadata:
  type: feedback
---

The recurring "header says 133 but true total is 128" class hit 4 times. The fix: add a `const declaredAssertions = N` plus a `TestAssertionCountHeader` that reads the file's line 2 and fails if it doesn't contain `fmt.Sprintf("Current assertion count: %d.", declaredAssertions)`.

Pattern (in `test/cenvkit-acceptance_test.go`):

```go
const declaredAssertions = 128

func TestAssertionCountHeader(t *testing.T) {
    f, err := os.Open("cenvkit-acceptance_test.go")
    ...
    sc := bufio.NewScanner(f)
    var line2 string
    for i := 0; sc.Scan(); i++ {
        if i == 1 { line2 = sc.Text(); break }
    }
    want := fmt.Sprintf("// cenvkit binary directly. Current assertion count: %d.", declaredAssertions)
    if line2 != want {
        t.Fatalf("stale-count mismatch: ...")
    }
}
```

**Why:** Every time someone adds assertions they bump the const (which is referenced in code) but forget the header comment, or vice versa. The test makes them a single atomic operation.

**How to apply:** When adding new assertions, bump `declaredAssertions` AND update line 2. The test fails fast if they diverge, before code review. Requires `"bufio"` and `"fmt"` imports.
