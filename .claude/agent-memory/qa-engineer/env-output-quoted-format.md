---
name: env-output-quoted-format
description: cenvkit env output wraps values in double-quotes (dotenv format); assert CI="true" not CI=true
metadata:
  type: feedback
---

`cenvkit env` emits dotenv-quoted output: `CI="true"`, `IS_DEV="false"`, not bare `CI=true`.

**Why:** The `envmap.Emit` function uses dotenv quoting for the default format; values are wrapped in `"..."`. Asserting `CI=true` (unquoted) will always fail even when the value is correct.

**How to apply:** In acceptance tests asserting on `cenvkit env` output, use backtick string literals with quotes: `` `CI="true"` `` not `"CI=true"`. This applies to all key=value assertions on env command output in dotenv format.
