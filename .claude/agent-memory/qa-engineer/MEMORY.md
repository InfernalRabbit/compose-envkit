# QA Engineer Memory Index

- [Plan Step 1b import artifact](plan-step1b-import-artifact.md) — plan's Step 1b appends `import "bytes"` as inline text; fold into file-level import block
- [discover_test.go uses package engine](discover-test-package.md) — white-box test calls unexported resolveComposeFiles; must be package engine, not engine_test
- [engine_test.go is package engine_test](engine-test-black-box.md) — black-box test imports the engine package; must be package engine_test
- [Fixture env file basenames](fixture-basenames.md) — confirmed on-disk Layer-2 env files in examples/monorepo
- [YAML flow seq interpolation](yaml-flow-seq-interpolation.md) — `${VAR}` inside YAML flow sequences breaks the parser; use block form
- [Secrets-last scope](secrets-last-scope.md) — secrets-last is Layer-1 only; assert via --chain, not env-files merged output
- [InChain not set in chain-only mode](inchain-chain-only-mode.md) — provenance.go early return skips gap-detection; InChain=false for all vars in chain-only mode
