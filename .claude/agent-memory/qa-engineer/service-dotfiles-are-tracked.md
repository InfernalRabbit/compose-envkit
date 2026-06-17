---
name: service-dotfiles-are-tracked
description: examples/monorepo service dotfiles (web/.web.env etc.) are git-tracked; only ROOT dotfiles are gitignored and need seeding
metadata:
  type: project
---

In `examples/monorepo`, there are two tiers of env files:

- **Root dotfiles** (`.env`, `.dev.env`, `.prod.env`) — gitignored; only `example.*` templates are committed. `stageMonorepo` seeds them by copying `example.env` → `.env` etc.
- **Service dotfiles** (`web/.web.env`, `web/.web.dev.env`, `web/.web.prod.env`, `api/.api.env`, `services/reports/.reports.env`) — **git-tracked** and already present after `stageMonorepo`'s `cp -R`.

**Why:** Service env files contain no secrets and are needed for the runnable blueprint to work out of the box. The root chain files ARE gitignored to demonstrate that users must seed them from templates.

**How to apply:** Never write seeding logic for `web/.web.env` or other service dotfiles — they're already staged by `cp -R`. An `os.ReadFile("web/example.web.env")` will always fail (that file does not exist), so any fallback that overwrites the real tracked file with a stub silently destroys it. Always verify git-tracked vs gitignored status before writing seeding logic. [[fixture-basenames]]
