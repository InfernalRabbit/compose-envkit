---
name: cli-flag-mutual-exclusivity-not-a-defect
description: env-debug view-mode flags are silent-precedence by design (v1 parity); demanding mutual-exclusivity validation is a feature request, not a plan defect
metadata:
  type: project
---

cenvkit `env-debug` view-mode flags (`--chain/--diff/--effective/--files/--trace/--value`)
resolve via a `switch` with SILENT PRECEDENCE — one mode wins, no error on multiple.
This is the established v1 behavior (cmd/cenvkit/main.go newEnvDebugCmd switch) and the
v2 provenance plan faithfully preserves it (plan RenderHuman switch + T3 HumanOpts).

**Why:** The spec's Error-handling section (§9, design doc) enumerates exactly the error
cases (no compose file → chain-only report; malformed compose → fatal; missing chain file
→ skipped; `--trace VAR` for an unset/empty var → reported "not set", NOT an error). It
does NOT list "multiple flags set" or "--trace/--value without --var" as errors. Adding
`MarkFlagsMutuallyExclusive` / required-var validation would CONTRADICT §9:182 and diverge
from v1 parity.

**How to apply:** A finding that asks for flag mutual-exclusivity or var-required errors on
env-debug is a UX/feature request (default real=false), not a correctness/compile defect —
unless the spec changes to mandate it. The empty-var "silent degrade to default view" in v2
(pick() returns "" → switch falls to chain) is a minor behavior nuance vs v1, also not an
error per §9. The REAL provenance-plan defects live elsewhere — see
[[provenance-plan-spec-drift]] (dropped ProvenanceFacts/Build/chain-A type names) and
[[env-debug-layer-scope-per-mode]] (--value Layer-1-only vs --trace Layer-2-rooted).
