# QA Engineer Memory Index

- [Plan Step 1b import artifact](plan-step1b-import-artifact.md) — plan's Step 1b appends `import "bytes"` as inline text; fold into file-level import block
- [discover_test.go uses package engine](discover-test-package.md) — white-box test calls unexported resolveComposeFiles; must be package engine, not engine_test
- [engine_test.go is package engine_test](engine-test-black-box.md) — black-box test imports the engine package; must be package engine_test
- [Fixture env file basenames](fixture-basenames.md) — confirmed on-disk Layer-2 env files in examples/monorepo
- [YAML flow seq interpolation](yaml-flow-seq-interpolation.md) — `${VAR}` inside YAML flow sequences breaks the parser; use block form
- [Secrets-last scope](secrets-last-scope.md) — secrets-last is Layer-1 only; assert via --chain, not env-files merged output
- [InChain not set in chain-only mode](inchain-chain-only-mode.md) — provenance.go early return skips gap-detection; InChain=false for all vars in chain-only mode
- [Overview gap text in render tests](overview-gap-text-in-render.md) — renderOverview gap line contains "NOT in the Layer-1 chain"; check for `interpolation:` prefix to distinguish trace-mode text; example.dev.env only has IS_DEV=true
- [Service dotfiles are git-tracked](service-dotfiles-are-tracked.md) — web/.web.env etc. are tracked and staged by stageMonorepo cp -R; only root .env/.dev.env are gitignored/seeded; never overwrite service dotfiles with stubs
- [DoD gate: gofmt -l . must be clean](dod-gate-gofmt.md) — go test alone is not enough; always run `gofmt -l . && go test ./... -count=1` before re-freezing
- [t.Setenv("K","") does NOT unset the key](feedback-tsetenv-vs-unsetenv.md) — use os.Unsetenv+cleanup for in-process tests; envWithout() for binary acceptance; masking gaps with empty vars is a subtle failure mode
