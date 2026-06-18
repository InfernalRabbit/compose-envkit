---
name: rename-overreach-compose-env-files
description: Blind s/COMPOSE_ENV/CENVKIT_ENV/ during the C3 rename caught COMPOSE_ENV_FILES → nonexistent CENVKIT_ENV_FILES; grep the bad token after any COMPOSE_ENV rename
metadata:
  type: feedback
---

When renaming cenvkit's `COMPOSE_ENV` selector → `CENVKIT_ENV`, a naive
substitution ALSO corrupts `COMPOSE_ENV_FILES` (the REAL docker-compose variable,
which must NEVER be renamed) into the nonexistent `CENVKIT_ENV_FILES`. C3 (#17,
qa) did exactly this in `test/seam_test.go` (5 instances at lines 10/94/117/134/140
— all in comments + `t.Fatal` message strings, so non-breaking but factually wrong
and a direct violation of the "NO COMPOSE_ENV_FILES rename" invariant).

**Why:** `COMPOSE_ENV` is a strict prefix of `COMPOSE_ENV_FILES`, so any
`s/COMPOSE_ENV/CENVKIT_ENV/` (no word-boundary, no `_FILES` negative-lookahead)
silently rewrites the real var. The build stays green because the corruption lands
in strings/comments, so unit tests + gofmt + vet ALL pass — only a targeted grep
catches it. Relates to [[has-compose-file-gate-seam]] (the COMPOSE_* family is
easy to conflate).

**How to apply:** after ANY COMPOSE_ENV→CENVKIT_ENV rename cycle, run
`grep -rn CENVKIT_ENV_FILES internal/ cmd/ test/ examples/` — it MUST return zero
(no such var exists). Also re-assert `COMPOSE_ENV_FILES` count is unchanged from
the pre-rename baseline. The correct sed has a boundary:
`s/COMPOSE_ENV\b/CENVKIT_ENV/g` (or `s/COMPOSE_ENV([^_])/CENVKIT_ENV$1/g`). This is
cosmetic-severity when confined to messages but ALWAYS flag it — it's the exact
"real compose var untouched" class the C3 task gated on.
