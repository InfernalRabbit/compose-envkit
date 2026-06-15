# Memory Index — code-reviewer

- [Circular interpolation class](spec-circular-interpolation-class.md) — cenvkit's reason-to-exist is a compose env_file/interpolation circularity; watch single-pass engine designs
- [Carried bug classes](carried-bug-classes-cenvkit.md) — host-token injection + secret-wipe caught in legacy; guards must be RED on pre-fix code
- [Glob-vs-include acceptance class](glob-vs-include-acceptance-class.md) — legacy discovery is find-by-glob; compose-go is include-only; scenarios 9/11/22 assert non-included strays → must invert/drop
- [Plan consistency defect classes](plan-consistency-defect-classes.md) — cenvkit plan recurring defects: dep-graph label drift, --value Layer-2 leak, COMPOSE_FILE seam
- [S4 acceptance count drift](s4-acceptance-count-drift.md) — spec hardcodes "61 exact" but G1-G5 inversions change the count; plan defers the recount as a TODO (re-opens S4)
- [compose-go option order + COMPOSE_FILE](compose-go-option-order-and-compose-file.md) — WithEnv must precede WithConfigFileEnv; WithConfigFileEnv does NOT interpolate COMPOSE_FILE (probe-verified v2.11.0)
- [env-debug layer scope per mode](env-debug-layer-scope-per-mode.md) — --value is Layer-1 ONLY; --trace is Layer-2-rooted (resolves into Layer-1). Don't collapse them — smoke.sh:213 traces a Layer-2-only var
- [HasComposeFile gate seam](has-compose-file-gate-seam.md) — untested Layer-2 gate reimplements COMPOSE_FILE separator (invents `,`), diverges from compose-go, can silently skip Layer-2
- [Seam check go list -deps false positive](seam-check-go-list-deps-false-positive.md) — the "only internal/engine imports compose-go" gate uses -deps → always RED; drop -deps, restrict to $(go list -m)
- [Monorepo fixture needs seeding](monorepo-fixture-needs-seeding.md) — examples/monorepo has no Layer-1 dotfiles on disk; acceptance must copy-to-temp + seed example.*→.*
- [validate --all chain re-resolve](validate-all-chain-reresolve-class.md) — per-env ops must inject COMPOSE_ENV into chain Input, not just docker subprocess; legacy DC_PROD proves prod must re-resolve `.prod.env`
- [Spec sketch carries option-order defect](spec-sketch-carries-option-order-defect.md) — the broken WithConfigFileEnv-before-WithEnv + false "honors COMPOSE_FILE" lives in SPEC §4 (lines 115-122) too, not just the plan; fix the authoritative spec
- [COMPOSE_FILE path untested at unit layer](compose-file-path-untested-at-unit-layer.md) — engine tests all use default discovery; interpolated COMPOSE_FILE overlay has no docker-free guard; scenario 15 is docker-skippable
- [WithConfigFileEnv no-interp + cwd-relative](withconfigfileenv-no-interp-cwd.md) — probe-confirmed: drops ${VAR} overlay AND resolves rel paths vs process cwd; scenario 15 via exec-docker stays green (misattributed guard)
- [DisableFlagParsing persistent-flag leak](disableflagparsing-persistent-flag-leak.md) — `compose` DisableFlagParsing:true → --project-dir leaks to docker & resolveProjectDir falls to cwd; also missing dc.Dir vs legacy cd $PROJECT_DIR (probe-verified, not in 61 suite)
