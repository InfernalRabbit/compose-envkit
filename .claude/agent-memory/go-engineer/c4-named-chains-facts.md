---
name: c4-named-chains-facts
description: C4 named-chains (INI [name] sections in .cenvkit.envchain + --chain <name> selector) blast radius, the --chain flag collision resolution, and the chain.Input/Result contract change
metadata:
  type: project
---

Cycle 4 = additive named chains. User decision (2026-06-19): GENERAL FLEXIBILITY
— standalone `[name]` sections, NO inheritance from `[default]`, NO compose
binding. Spec §7b.

**Format:** optional INI-style `[name]` headers in `.cenvkit.envchain`. Lines
before any header (or a header-less file) = implicit `[default]` chain
(back-compat with today's flat format, e.g. `examples/monorepo/.cenvkit.envchain`
which is header-less). Each `[name]` is a COMPLETE STANDALONE ordered file list.

**THE COLLISION (RESOLVED → option a):** `env-debug` already uses `--chain` as a
BOOLEAN MODE = the Layer-1 chain-files list, which is ALSO the `default:` case in
`RenderHuman` (render.go:116). So `env-debug --chain` == bare `env-debug`. The
named-chain selector is a STRING `--chain <name>`. Resolution: RENAME env-debug's
boolean `--chain` to `--list` (cmd/cenvkit/main.go:397 BoolVar + :344 mChain +
:387 `Chain: mChain`), freeing `--chain <name>` as the ONE universal string
selector everywhere. Pre-1.0, clean-break (matches C3 philosophy). Architect owns
the CHANGELOG note. `provenance.HumanOpts.Chain` (model/render fields) stays — it
is the internal view selector, NOT the flag; only the cobra flag NAME changes.

**chain.Input/Result contract change:** add `Chain string` to `chain.Input`
(empty = "default"). `readChainTemplates(projectDir)` → must parse sections +
take the selected section name; return an error type carrying available names
when the requested section is absent (cmd maps to exit 2 via exitError). Result
gains nothing new — the selected section's file list flows through the EXISTING
`Result.Files` unchanged, so the engine seam + envmap + envfiles need ZERO
changes (named chains only change WHICH list chain.Resolve produces).

**Per-command --chain wiring (8 commands):** `run`, `env`, `gap-report`,
`compose`, `validate`, `env-files`, `env-debug` (the universal string selector).
Threading: a shared helper reads the flag and passes `Chain:` into chain.Input.
- `assemble()` (main.go:122) + `resolvePopulator()` (main.go:149) need a chain
  arg → thread into chain.Input.Chain.
- `compose` has `DisableFlagParsing: true` (main.go:219) → `--chain <name>` must
  be MANUALLY pre-scanned + stripped like `--project-dir` is via
  `extractProjectDir` (main.go:258), else docker rejects it. Add an extractor or
  generalize extractProjectDir.
- env-debug/gap-report call `chain.Resolve` inline (main.go:355,433) → add the
  flag + pass Chain.

**Errors:** `--chain <name>` absent from file → exit 2 + list available names.
`--chain` on a header-less file resolves only `default` (any other name → exit 2).
`--chain default` always valid (header-less or `[default]`-present).

**Test guard:** bump `declaredAssertions` (test/cenvkit-acceptance_test.go:63,
currently 128) AND the line-2 header comment together when qa adds acceptance
assertions — TestAssertionCountHeader enforces both.

**SHIPPED (probe-verified against built binary 2026-06-19):**
- Chose option (a) + persistent root flag (lead's refinement). `--chain` is a
  PERSISTENT root flag (main.go ~:68, mirrors --project-dir) read via
  `resolveChainName(cmd)` (~:123) — ONE definition, inherited everywhere; NOT 8
  per-command flags. version/init inherit it harmlessly (like --project-dir).
- env-debug bool `--chain` → `--list` (local var mChain→mList; `HumanOpts.Chain:
  mList` internal field UNCHANGED). Verified: `--list` == bare env-debug; old bool
  `--chain` (no arg) now errors "flag needs an argument"; `--chain api` selects.
- `extractProjectDir` generalized to `extractPersistentFlag(args, name)`;
  `extractProjectDir` KEPT as a thin wrapper (qa's main_test.go:606 calls it by
  name — renaming would red their test). compose strips BOTH --project-dir AND
  --chain (both `--flag VAL` and `--flag=VAL` forms) before forwarding to docker —
  verified docker compose --help prints normally, no unknown-flag rejection.
- main() maps `*chain.UnknownChainError` → exit 2 (errors.As branch before the
  generic exit-1). Error msg: `no chain named "x" (available: api, web)` or
  `(this file defines no named chains; only "default" is available)`.
- Parser quirk (intentional): pre-header lines AND a later explicit `[default]`
  section CONCATENATE into one default chain (both append to sections["default"]).
- `availableChains` omits "default" from the listing (always resolves → noise).
- Token orthogonality verified: `--chain web` × `CENVKIT_ENV=prod` independent —
  `${CENVKIT_ENV}` still reselects the file WITHIN the chosen section.

**qa tests awaiting (I list, qa writes — *_test.go is their zone):** chain unit
(header-less⇒default; standalone no-inheritance; missing-section UnknownChainError
w/ sorted names; default valid both ways; empty default chain ⇒ nil ⇒ exit 0);
cmd/acceptance (--chain selects different list across run/env/env-files; compose
strips --chain; env-debug --list==bare regression + old --chain-as-bool now needs
arg; missing-section exit 2). Bump declaredAssertions + line-2 comment together.

Related: [[c3-clean-rename-facts]] (the file/selector this extends),
[[provenance-chain-vs-files-view]] (the --chain/--files boolean view distinction).
