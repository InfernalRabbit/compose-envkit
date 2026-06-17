---
name: overview-fixture-seeding-and-missing-chain-test
description: --overview review — plan-promised chain ComposeEnvSource test was skipped; acceptance seeding block clobbers the git-tracked web/.web.env fixture with a stub; monorepo service dotfiles ARE tracked (root ones gitignored)
metadata:
  type: project
---

cenvkit `env-debug --overview` review (2026-06-17) surfaced two recurring
test-fidelity defect patterns worth watching on every cenvkit increment:

1. **Plan-promised test silently skipped.** Plan T2b + spec §8a both said the new
   `chain.Result.ComposeEnvSource` ships "with a chain unit test." It was NOT
   added — `internal/chain/chain_test.go` unchanged; the field is only referenced
   as a hardcoded fixture label in `render_test.go`. The three branches in
   `resolveComposeEnv` (shell/.env/default) had zero direct coverage. ALWAYS grep
   `--include=*_test.go` for any new prod field/branch a plan promised to test.

2. **Fixture-seeding block that masks real committed data.** Acceptance
   `TestOverview_RuntimeWebLayer`/`_WEBPORTGap` read `web/example.web.env` (which
   does NOT exist) and fall back to writing a minimal `WEB_PORT=18080` stub —
   overwriting the real, git-TRACKED `examples/monorepo/web/.web.env` that
   `stageMonorepo`'s `cp -R` already staged. Assertions still pass (presence/gap),
   but the stub silently replaces richer fixture content.

**KEY FIXTURE FACT (corrects [[monorepo-fixture-needs-seeding]]):** in the monorepo
example, ROOT-level dotfiles (`.env`/`.dev.env`/`.prod.env`) are **gitignored**
(only `example.*` tracked → must seed) BUT SERVICE-level dotfiles
(`web/.web.env`, `.web.dev.env`, `.web.prod.env`) are **git-tracked** and present
on disk after `stageMonorepo`. So service env_file content needs NO seeding;
seeding it is dead/destructive. There is no `web/example.web.env`.

**Why:** acceptance tests drive the real binary against a copied fixture; a
seeding block written by analogy to the root-level seeding wrongly assumes the
service files are also example-templated.

**How to apply:** when reviewing cenvkit acceptance tests that touch
examples/monorepo, check `git ls-files` + `git check-ignore` to know which files
are staged vs need seeding; flag any seed/fallback that overwrites a tracked
fixture file.
