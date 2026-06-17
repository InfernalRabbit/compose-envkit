---
name: overview-gap-text-in-render
description: renderOverview gap line contains "NOT in the Layer-1 chain" — don't assert its absence in overview tests
metadata:
  type: feedback
---

The overview renderer (renderOverview) emits its gap annotation as:
`⚠ gap: WEB_PORT — used as ${WEB_PORT} in service web ... but NOT in the Layer-1 chain → run falls back.`

This line contains "NOT in the Layer-1 chain" — the same substring the renderTrace gap path uses for its `interpolation: NOT in the Layer-1 chain` line.

A test that checks `!strings.Contains(got, "NOT in the Layer-1 chain")` in an overview output WILL fail because the gap annotation contains this text.

**Why:** The overview's gap line and the trace's gap line share wording; the trace-specific prefix is `interpolation:` (prefixed with the `interpolation:` label).

**How to apply:** When asserting that overview output does NOT contain trace-mode text, check for the trace-specific prefix `"interpolation: NOT in the Layer-1 chain"` (with the `interpolation:` label), NOT the bare `"NOT in the Layer-1 chain"` substring.

Also: `examples/monorepo/example.dev.env` only has `IS_DEV=true`; it does NOT override SITE_URL. Don't write acceptance tests that expect `~ SITE_URL` from the monorepo fixture — use a scratch fixture instead. [[fixture-basenames]]
