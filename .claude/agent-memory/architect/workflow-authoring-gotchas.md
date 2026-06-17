---
name: workflow-authoring-gotchas
description: Authoring Workflow scripts — escape ${...} in prompt template literals (JS interpolates them); Date.now/Math.random unavailable; pass args as real JSON
metadata:
  type: feedback
---

When authoring a `Workflow` script (JS), watch these — they bite instantly:

- **Escape `${...}` inside backtick prompt strings.** Prompt text that mentions
  compose/env tokens like `${ENV}`, `${COMPOSE_ENV}`, `${WEB_PORT}` lives inside JS
  **template literals**, so JS tries to interpolate them → `ReferenceError: ENV is
  not defined` and the workflow fails in ~17ms with 0 agents. Write `\${ENV}` (or
  avoid the `${}` form in prose). Keep REAL interpolations (`${d.key}`,
  `${maps.length}`) unescaped. **Why:** 2026-06-17, the example-test-gap-analysis
  workflow failed on launch on exactly this; relaunch with `\${...}` worked.
- **No `Date.now()` / `Math.random()` / argless `new Date()`** in scripts (they
  throw — would break resume). Stamp timestamps after the workflow returns; vary
  per-agent labels by index, not randomness.
- **Pass `args` as actual JSON** (arrays/objects), not a JSON-encoded string.
- **The workflow's RETURN value** comes back to me (in the task-notification, often
  truncated). Read the full result from the output file
  (`/tmp/.../tasks/<id>.output`) — that's the result JSON, NOT the agent transcript
  (the transcript files are the ones not to cat).

**How to apply:** when a substantive task (gap analysis, multi-angle audit,
review fan-out) warrants Workflow under ultracode, scaffold the script with these
in mind; iterate by editing the persisted `scriptPath` and re-invoking, not by
resending. See also [[verify-committed-tree-during-concurrent-edits]] (verify the
workflow's findings on disk before acting).
