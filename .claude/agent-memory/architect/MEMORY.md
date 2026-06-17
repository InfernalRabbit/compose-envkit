# Memory Index

- [Layer 2 is debug-only now](layer2-debug-only-not-populated.md) — 2026-06-17 reversal: run path drops service env_file: from COMPOSE_ENV_FILES; env_file=runtime-only; Layer 2 repurposed to env-debug gap-detector
- [Thin engine — compose owns resolution](thin-engine-compose-owns-resolution.md) — cenvkit only assembles the file list; docker compose owns env_file resolution + variable precedence (don't re-engineer it) — NOTE: Layer-2-in-run-list superseded by [[layer2-debug-only-not-populated]]
- [Verify committed tree, not working tree](verify-committed-tree-during-concurrent-edits.md) — during concurrent edits, test the staged subset (stash -u) before declaring a commit green; a green working-tree test can ship a broken HEAD
- [SmartDriver SH kit already current](smartdriver-sh-kit-already-current.md) — 2026-06-17: don't migrate/re-source SmartDriver's sh kit (already ≈v0.2.0 + better-adapted); SH is frozen/EOL, only Go cenvkit has a future
- [Workflow authoring gotchas](workflow-authoring-gotchas.md) — escape ${...} in workflow prompt template literals (JS interpolates → instant fail); no Date.now/rand; read full result from the output file
