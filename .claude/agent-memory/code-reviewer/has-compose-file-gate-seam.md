---
name: has-compose-file-gate-seam
description: cenvkit's HasComposeFile gate reimplements COMPOSE_FILE separator/abs logic, untested, and diverges from compose-go separator semantics — gate can disagree with the loader and silently skip Layer-2
metadata:
  type: project
---

cenvkit plan (`docs/superpowers/plans/2026-06-15-cenvkit-v1-implementation.md`)
Task 3 Step 5 defines `engine.HasComposeFile(dir, composeFileEnv)` — the
production gate that decides whether Layer-2 runs at all (call site Task 6
~line 1359: `if HasComposeFile(...) { ...Resolve... }`). Two defects ride together:

1. **Untested.** Plan table (line ~70) marks it "no" test; the only Task 3 RED
   test is the D1 MissingRequired test. The COMPOSE_FILE-present branch (the
   separator split + abs-path stat) has ZERO unit coverage; G4 acceptance only
   hits the no-compose-file `false` return.
2. **Separator divergence + gate/loader seam drift.** HasComposeFile invents a
   `,` separator heuristic; compose-go uses `COMPOSE_PATH_SEPARATOR` else
   `os.PathListSeparator` (`:`), NEVER `,` (see
   [[compose-go-option-order-and-compose-file]]). It also stats the RAW value
   without interpolation, while `Resolve` (Step 9) may interpolate
   `${COMPOSE_ENV}`. So the gate and the actual loader independently reimplement
   "does COMPOSE_FILE resolve" and can disagree → a COMPOSE_FILE listing only an
   interpolated overlay makes the gate stat `${...}` literally, return false, and
   silently skip Layer-2 (the tool's reason to exist). Fixture
   `examples/monorepo/example.env:18` =
   `docker-compose.yml:docker-compose.${COMPOSE_ENV}.yml` only passes the gate by
   accident (first entry exists).

**Why:** This is the recurring cenvkit defect class — a small piece of "compose
semantics" reimplemented outside the compose-go seam, green on each side, drifting
between layers. Relates to [[carried-bug-classes-cenvkit]] and
[[plan-consistency-defect-classes]] (COMPOSE_FILE seam).

**How to apply:** When reviewing any cenvkit gate/helper that parses COMPOSE_FILE
or compose discovery, demand (a) a RED-then-GREEN table test that fails on the
undefined symbol, and (b) that separator + interpolation match compose-go exactly,
ideally by sharing one resolver between the gate and Resolve so they cannot drift.

**Re-confirmed 2026-06-15 (composego-fidelity reviewer finding, verdict real):**
Plan lines 768-771 = the invented `,` separator heuristic; lines 772-782 = stat of
RAW (uninterpolated) entries. The token-only-COMPOSE_FILE break is concrete: a
project with `COMPOSE_FILE=docker-compose.${COMPOSE_ENV}.yml` (single token entry)
makes the gate stat `${...}` literally → false → Layer-2 silently skipped. The
monorepo fixture (`example.env:18`) passes ONLY because its FIRST entry
`docker-compose.yml` is token-free and short-circuits true at line 780. Asymmetry
inside the plan itself: `Resolve` Step 9 (lines 888-889) DOES plan to interpolate
`${COMPOSE_ENV}`/`${ENV}` on COMPOSE_FILE; the gate does not — same-plan seam
drift. Fix: gate must (i) use COMPOSE_PATH_SEPARATOR-else-os.PathListSeparator
(drop `,`), (ii) interpolate `${COMPOSE_ENV}`/`${ENV}` from seed env before stat,
ideally sharing one resolver with Resolve. Note: finding labeled "minor" but the
token-only break is a correctness defect (silent chain-only), not a nit.
