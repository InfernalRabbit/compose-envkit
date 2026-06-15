---
name: carried-bug-classes-cenvkit
description: legacy review caught a host-token injection and a secret-wipe in the sh kit — watch these classes carry into the Go rewrite
metadata:
  type: project
---

The legacy compose-envkit review already caught two real bugs that the Go rewrite
must not reintroduce:

1. **Host-token injection** — a `|`/`&` hostname crashed the sh engine via
   sed-based substitution (rewrite spec §1). The Go `internal/chain` still
   interpolates host-derived `${HOST}`/`${HOSTNAME}` into paths ("pure Go strings"
   removes the *sed* vector, NOT the validation need). Watch for `,` (the
   `COMPOSE_ENV_FILES` separator) and `..`/absolute paths in substituted values.
2. **Secret-wipe** — `cenvkit init` no-clobber seed must NOT overwrite an existing
   populated `.secrets.env`.

**Why:** Carried-bug-class regressions slip through because each layer's unit test
is green; the bug lives in the seam / in unsanitized input.

**How to apply:** Every remediation guard for these MUST be RED on pre-fix code
(temp-revert check) — a guard green from birth proves nothing. Demand a failing
test for: hostname `a,b`/`a|b`; init against non-empty `.secrets.env`. See
`.claude/artifacts/spec-audit.md` W1+W5. Also: secrets stay last-wins
(`docs/concepts.md:93` emit order). Related: [[spec-circular-interpolation-class]].
