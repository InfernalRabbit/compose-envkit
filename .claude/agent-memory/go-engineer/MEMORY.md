# Memory Index

- [compose-go API facts](compose-go-api-facts.md) — verified v2.11.0 loader path, EnvFiles, gotchas + how to verify via go doc when context7 is down
- [compose-go B-lite provenance](compose-go-blite-provenance.md) — proven v2.11.0 mechanism (var->service/field->resolved), normalization+warning traps, C separability hinges on D1 lever
- [D1 lenient-enumeration lever](d1-lenient-enumeration-lever.md) — cli.WithoutEnvironmentResolution makes LoadProject tolerate a missing required env_file while keeping EnvFiles paths (proven v2.11.0)
- [resolveComposeFiles standard-name fallback](resolvecomposefiles-standard-name-fallback.md) — engine resolver must discover standard compose names when COMPOSE_FILE unset; qa RED test is the binding contract over the plan sketch
- [monorepo fixture Layer-1 needs seeding](monorepo-fixture-layer1-needs-seeding.md) — examples/monorepo .env/.dev.env etc are gitignored (only example.* on disk); copy to tmp + `cenvkit init` to see Layer-1; never edit the committed fixture
- [distribution config facts](distribution-config-facts.md) — shim/binary name collision, goreleaser v2 + golangci v2 formats, .gitignore lead-owned, validate YAML via in-module yaml lib when tools absent
- [cobra persistent flag read-from-root](cobra-persistent-flag-read-from-root.md) — read persistent flags via cmd.Root().PersistentFlags().Get; cmd.Flags() omits them on root (silent fall-through-to-default bug)
- [provenance --chain vs --files view](provenance-chain-vs-files-view.md) — env-debug --chain = Layer-1 only (secrets-last, acceptance 12.4); plan's RenderHuman collapsed --chain/--files to full Report.Files; fix via HumanOpts.ChainFiles []string from cmd
- [macOS sed/xargs gotcha](macos-sed-xargs-gotcha.md) — BSD sed -i needs '' arg; grep -lZ|xargs -0 sed collapses filenames; use find -exec + per-file loop for bulk in-file swaps
