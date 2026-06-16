---
name: provenance-diff-flag-dead-alias
description: v2 provenance plan declares env-debug --diff as "alias of --effective" but never routes mDiff into HumanOpts (_ = mDiff) → dead flag + false self-review; spec §7 omits --diff entirely
metadata:
  type: project
---

In the v2 rich-provenance plan (docs/superpowers/plans/2026-06-16-cenvkit-rich-provenance.md),
Task 3 declares `--diff` (plan line 656, help "alias of --effective (back-compat)")
and the Self-review (line 729) claims it's "retained as a back-compat alias of
--effective". But the RunE does `_ = mDiff` (line 651) and never puts mDiff into
the `HumanOpts` passed to `RenderHuman`. RenderHuman's switch (plan lines 185-201)
has no Diff case, so `env-debug --diff` falls to `default` → prints r.Files (the
file list), NOT per-service effective env. The flag is DEAD and the self-review
claim is FALSE. Confirmed real=true (major).

**Why it's a genuine defect (not style):** internal contradiction between code
(line 651) and stated contract (line 729) + a misleading `--help` string a user
will trust. No acceptance catches it — smoke.sh:210 `run_edbg "--diff"` asserts
EXIT 0 only (run_edbg checks exit code, not output), and `--diff` still exits 0
via the default branch.

**Two nuances the finding got slightly wrong but that don't change the verdict:**
- v1 `--diff` was `debug.Diff` (Layer-2-over-Layer-1 var diff, `+ KEY=VAL`),
  NEVER an alias of `--effective` (which was `docker compose config`). So the
  self-review's "alias of --effective" premise was invented, not a v1 fact.
- Spec §7 (design doc lines 157-165) lists only --trace/--effective/--chain/
  --files/--value; `--diff` is OMITTED from the v2 CLI surface entirely.

**Preferred fix:** (b) drop the alias claim, remove the dead flag, document
--diff as removed (matches spec §7 which already omits it). (a) `mEffective =
mEffective || mDiff` would make the claim TRUE but aliases to the wrong v1
semantics; only acceptable if a deliberate decision + an acceptance asserting
--diff == --effective output. Sibling to [[cli-flag-mutual-exclusivity-not-a-defect]]
(that one was real=false; THIS one is real=true — the difference is a dead flag +
false self-review vs a UX feature request).
