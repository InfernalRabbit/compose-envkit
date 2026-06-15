# D1 lever — make a missing *required* env_file load succeed (compose-go v2.11.0)

go-engineer · READ-ONLY research · 2026-06-15. No code written into the repo;
all probing in throwaway `/tmp/cenvkit-d1-probe/`. context7/serena not used —
source-of-truth method is `go doc` + a real `LoadProject` against a fixture
(verified primary-source probe).

## TL;DR (the lever)

Use **`cli.WithoutEnvironmentResolution`** as a `ProjectOptionsFn`. It is the
clean public wrapper for `loader.Options.SkipResolveEnvironment = true`. Under it:

- a MISSING but `required: true` env_file does **NOT** abort `LoadProject`;
- every service's `EnvFiles[].Path` is **still fully populated** (including the
  missing file's absolute path);
- path **interpolation still runs** (`${VAR}` in an env_file path resolves), as
  does normalization, `include:`, profiles, and absolute-path resolution.

This is exactly the D1 "lenient enumeration" knob. The engine then drops
non-existent paths itself (compose-go keeps the path string; it just never reads
the file under this option). The real `docker compose` run later (no lever)
enforces `required:` faithfully.

---

## 1. Pinned version (proof)

```
$ go list -m github.com/compose-spec/compose-go/v2
github.com/compose-spec/compose-go/v2 v2.11.0
```
Toolchain: `go version go1.26.3 darwin/arm64`. `go get …/v2@v2.11.0` (exact pin,
not `@latest`).

---

## 2. The EXACT lever — minimal compiling snippet (the one actually run)

```go
opts, err := cli.NewProjectOptions(
    []string{"/abs/compose.yaml"},
    cli.WithWorkingDirectory(dir),
    cli.WithConfigFileEnv,
    cli.WithDefaultConfigPath,
    cli.WithEnv(env),                  // Layer-1 chain -> path interpolation
    cli.WithResolvedPaths(true),       // EnvFile.Path => absolute
    cli.WithInterpolation(true),
    cli.WithoutEnvironmentResolution,  // <-- THE D1 LEVER
)
proj, err := opts.LoadProject(ctx)
// proj.Services[name].EnvFiles[i].Path  is populated for ALL files, incl. missing.
```

`cli.WithoutEnvironmentResolution` is a bare `func(*ProjectOptions) error`
(no call parens — pass the function value itself, like `cli.WithConfigFileEnv`).

Equivalent low-level form (same mechanism, use only if you need other loader
knobs at the same time):

```go
cli.WithLoadOptions(func(o *loader.Options) { o.SkipResolveEnvironment = true })
```

`cli.WithoutEnvironmentResolution` is preferred (one named option, no `loader`
import leaking into call sites).

---

## 3. Literal `go doc` / source evidence

### `cli` ProjectOptionsFn (v2.11.0) — relevant names
```
func WithoutEnvironmentResolution(o *ProjectOptions) error
    WithoutEnvironmentResolution disable environment resolution
func WithConfigFileEnv(o *ProjectOptions) error
func WithDefaultConfigPath(o *ProjectOptions) error
func WithDiscardEnvFile(o *ProjectOptions) error
    WithDiscardEnvFile sets discards the `env_file` section after resolving to the `environment` section
func WithEnv(env []string) ProjectOptionsFn
func WithResolvedPaths(resolve bool) ProjectOptionsFn
func WithInterpolation(interpolation bool) ProjectOptionsFn
func WithLoadOptions(loadOptions ...func(*loader.Options)) ProjectOptionsFn
    WithLoadOptions provides a hook to control how compose files are loaded
```
⚠️ **`WithDiscardEnvFile` is the WRONG lever** — its doc says it "discards the
`env_file` section after resolving"; it would WIPE `EnvFiles`. Do not use it.

### `loader.Options` — relevant fields
```
type Options struct {
    SkipValidation         bool   // Skip schema validation
    SkipInterpolation      bool   // Skip interpolation
    SkipNormalization      bool   // Skip normalization
    ResolvePaths           bool   // Resolve path
    SkipConsistencyCheck   bool   // Skip consistency check
    SkipResolveEnvironment bool   // SkipResolveEnvironment will ignore computing `environment` for services
    ...
}
```

### `cli.WithoutEnvironmentResolution` source (proves B == C)
`…/compose-go/v2@v2.11.0/cli/options.go:378`
```go
// WithoutEnvironmentResolution disable environment resolution
func WithoutEnvironmentResolution(o *ProjectOptions) error {
    o.loadOptions = append(o.loadOptions, func(options *loader.Options) {
        options.SkipResolveEnvironment = true
    })
    return nil
}
```

### Why this works — the single guarded site (root cause)
`…/v2@v2.11.0/loader/loader.go:606`
```go
if !opts.SkipResolveEnvironment {
    project, err = project.WithServicesEnvironmentResolved(opts.discardEnvFiles)
    ...
}
```
`WithServicesEnvironmentResolved` is the ONLY thing that stats env_file paths and
errors on a missing required one — `…/v2@v2.11.0/types/project.go:748`:
```go
func loadEnvFile(envFile EnvFile, environment Mapping, resolve dotenv.LookupFn) error {
    if _, err := os.Stat(envFile.Path); os.IsNotExist(err) {
        if envFile.Required {
            return fmt.Errorf("env file %s not found: %w", envFile.Path, err)
        }
        return nil
    }
    ...
}
```
The `EnvFiles` slice is built earlier (normalization), so skipping this step
leaves the paths intact while removing the stat/read that fails.

### `types.EnvFile` (v2.11.0)
```
type EnvFile struct {
    Path     string `yaml:"path,omitempty" json:"path,omitempty"`
    Required OptOut `yaml:"required,omitempty" json:"required,omitzero"`
    Format   string `yaml:"format,omitempty" json:"format,omitempty"`
}
type OptOut bool   // OptOut is a boolean which default value is 'true'
```

---

## 4. Empirical result — 2-service fixture with a missing required env_file

Fixture `compose.yaml`: service `web` has `env_file: [./present.env, {path: ./MISSING.env, required: true}]`
(present.env exists; **MISSING.env does NOT**); service `api` has
`env_file: ./interp-${PROBE_SUFFIX}.env` (load env seeds `PROBE_SUFFIX=xyz`,
present file `interp-xyz.env` exists). Program output:

```
===== A: BASELINE (no lever) =====
LOAD ERROR: env file /tmp/cenvkit-d1-probe/MISSING.env not found: stat … no such file or directory

===== B: cli.WithoutEnvironmentResolution =====
LOAD OK. WorkingDir=/tmp/cenvkit-d1-probe
  service "api": 1 EnvFiles
    - Path="/tmp/cenvkit-d1-probe/interp-xyz.env" Required=true Format=""
  service "web": 2 EnvFiles
    - Path="/tmp/cenvkit-d1-probe/present.env" Required=true Format=""
    - Path="/tmp/cenvkit-d1-probe/MISSING.env" Required=true Format=""

===== C: WithLoadOptions{SkipResolveEnvironment:true} =====
   (identical to B — same mechanism)

===== D: WithLoadOptions{SkipValidation:true} =====
LOAD ERROR: env file /tmp/cenvkit-d1-probe/MISSING.env not found: …
```

- **A (no lever): ERRORS** — confirms default required-fatal behavior.
- **B / C: LOAD OK**, both services' `EnvFiles[].Path` printed, including the
  absolute path of the missing `MISSING.env`.
- **D (`SkipValidation`): still ERRORS** — validation is NOT the gate;
  environment resolution is. SkipValidation is the wrong lever.

---

## 5. Is `svc.EnvFiles[].Path` populated under the lever? — **YES**

Evidence: §4 variant B prints all 3 EnvFile paths (web: present.env + MISSING.env,
api: interp-xyz.env) as absolute paths after a successful load. The missing
required file's path survives. (Implication for D1: the engine must filter
non-existent paths itself — compose-go keeps the path string under this option.)

---

## 6. Gotchas

1. **Path interpolation still happens under the lever — confirmed.** Separate
   probe (`/interp`) loaded the same fixture twice under
   `cli.WithoutEnvironmentResolution`:
   ```
   WITH PROBE_SUFFIX=xyz:  api env_file Path = ".../interp-xyz.env"
   WITHOUT PROBE_SUFFIX:   api env_file Path = ".../interp-.env"
                           (+ compose-go warning: "PROBE_SUFFIX is not set")
   ```
   So `${COMPOSE_ENV}` / `${SVC_DIR}` in env_file paths WILL interpolate from the
   `cli.WithEnv` Layer-1 chain (matches reviewer C1: paths interpolate from the
   load env, not from other services' Layer-2 contents). The lever only skips
   *reading env_file contents*, not interpolation.
2. **`Project.Services` is a Go map → engine MUST sort** for deterministic
   `COMPOSE_ENV_FILES` (unchanged from prior research; not a lever issue).
3. **Don't confuse with `WithDiscardEnvFile`** — that one nukes `EnvFiles`. Wrong.
4. **`SkipValidation` does NOT help** (variant D) — keep validation ON; only
   `SkipResolveEnvironment` / `WithoutEnvironmentResolution` is needed.
5. **Engine still drops missing paths itself** for D1 leniency: the lever makes
   the LOAD succeed and keeps the path, but a non-existent file is still in the
   list. `os.Stat`-filter the enumerated paths (skip-missing parity with the sh
   kit) before assembling `COMPOSE_ENV_FILES`; the real `docker compose` run
   (loaded WITHOUT the lever) re-enforces `required:`.
6. **Side effect to document:** under the lever `ServiceConfig.Environment` is NOT
   computed (that's the whole point). We don't read `Environment` for enumeration
   (we read `EnvFiles`), so this is fine — but note it if anything downstream ever
   wants the resolved per-service environment from the SAME load.

---

## Reproduction

`/tmp/cenvkit-d1-probe/` (throwaway): `go.mod` (pin v2.11.0), `compose.yaml`,
`present.env`, `interp-xyz.env`, `main.go` (variants A–D), `interp/main.go`
(interpolation proof). `go build ./...` + `go vet ./...` clean.
