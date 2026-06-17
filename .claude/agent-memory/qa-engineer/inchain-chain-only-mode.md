---
name: inchain-chain-only-mode
description: InChain field not set in chain-only mode (no compose file) — bug in provenance.go early return before gap-detection block
metadata:
  type: feedback
---

`internal/engine/provenance.go` has a chain-only early return (`if len(configs) == 0 { return rep, nil }`) that runs BEFORE the gap-detection block that sets `InChain`. In chain-only mode, every var gets `InChain=false` even if it is in Layer-1, causing `renderTrace` to show the "NOT in chain" path incorrectly.

**Why:** The gap-detection loop at the bottom of Provenance() sets `InChain` by checking `interpEnv`, but the early return for chain-only mode skips this block entirely.

**How to apply:** When testing `--trace` in chain-only fixtures (no compose file), verify `in_chain=true` in JSON output for Layer-1 vars. If the test sees "NOT in the Layer-1 chain" for a var that IS in the chain, check whether this early-return bug is still present. The fix is to populate InChain BEFORE the early return. [[secrets-last-scope]]
