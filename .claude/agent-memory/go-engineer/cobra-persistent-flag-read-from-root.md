---
name: cobra-persistent-flag-read-from-root
description: cobra persistent flags must be read via cmd.Flag(name) — cmd.Flags() omits persistent flags on the root receiver (silent fall-through bug)
metadata:
  type: feedback
---

A cobra PERSISTENT flag (registered with `root.PersistentFlags().String(...)`)
must be read with `cmd.Flag(name)` (preferred) or
`cmd.Root().PersistentFlags().GetString(name)`, NOT `cmd.Flags().GetString(name)`.
The lead's adopted form is `cmd.Flag("project-dir").Value.String()` (nil-guard the
Flag()): `cmd.Flag()` searches local+inherited+persistent flag sets without needing
a merge, so it's the idiomatic accessor that works for every receiver.

**Why:** `cmd.Flags()` on the ROOT command does NOT include its own persistent
flags — cobra only merges inherited persistent flags into a *subcommand's*
`Flags()` at parse time. So a helper like `resolveProjectDir(cmd)` that reads
`cmd.Flags().GetString("project-dir")` returns "" whenever cmd is the root (or any
context before merge), silently falling through to the default (cwd). The
`--project-dir` flag-set branch was effectively DEAD — an explicit `--project-dir
/other/dir` was ignored and cwd used instead. It only "worked" in tests where the
explicit dir happened to equal cwd. Found by qa's `TestProjectDirFlagWiring` which
calls `resolveProjectDir(root)` directly after `root.PersistentFlags().Set(...)`.

**How to apply:** Read persistent flags via `cmd.Root().PersistentFlags().Get*`.
Probe-verified (cobra v1.10.2): this works for every receiver — root, subcommand,
and the compose-path override where `newComposeCmd`'s RunE does
`cmd.Flags().Set("project-dir", v)` (that Set writes through to the SAME shared
root persistent flag, so a later `Root().PersistentFlags().Get` sees it). Lesson:
a "testable seam" (a helper callable on the root directly) catches dead-branch
fall-through bugs that an end-to-end test masks when the default coincides with the
explicit value. Verify flag plumbing with an explicit value that DIFFERS from cwd.
