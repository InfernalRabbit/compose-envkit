# compose-go API — research for `internal/engine`

Task #1 (go-eng). RESEARCH ONLY — no Go code written.

**Source of truth & verification method:** context7/serena MCP were not reachable
in this session, so I verified the API authoritatively against the **compose-go
source itself**. I downloaded the module into a throwaway probe (`go get
github.com/compose-spec/compose-go/v2@latest`) and ran `go doc` on the real
package. Every signature below is a literal `go doc` output, not a guess.

- **Module:** `github.com/compose-spec/compose-go/v2`
- **Version pulled by `@latest` on 2026-06-15:** **v2.11.0** (this is the version
  to pin in `go.mod`; bump deliberately per spec §10).
- **Go toolchain present:** go1.26.3 darwin/arm64.
- Note: there is a legacy non-`/v2` module (`compose-go` v1.20.2, pulled
  transitively). **Use `/v2`** — it is the actively developed line Docker Compose
  ships.

---

## 1. The two API layers

compose-go has two entry layers. We want the **`cli`** layer because it already
implements the env/`COMPOSE_FILE`/dotenv plumbing the sh kit hand-rolls.

### Layer A — `cli` (high-level, what we use)

Package `github.com/compose-spec/compose-go/v2/cli`.

```
func NewProjectOptions(configs []string, opts ...ProjectOptionsFn) (*ProjectOptions, error)
func (o *ProjectOptions) LoadProject(ctx context.Context) (*types.Project, error)
func (o *ProjectOptions) GetWorkingDir() (string, error)
```

- `ProjectFromOptions(ctx, *ProjectOptions)` exists but is **Deprecated** —
  docstring: *"use ProjectOptions.LoadProject or ProjectOptions.LoadModel"*.
  Use the **`LoadProject` method**.

Relevant `ProjectOptionsFn` options (all verified via `go doc`):

| Option | Purpose for cenvkit |
|---|---|
| `WithWorkingDirectory(wd string)` | project dir = where we run |
| `WithConfigFileEnv` | honor `COMPOSE_FILE` env (spec §4 step 3) |
| `WithDefaultConfigPath` | fall back to default `docker-compose.y*ml` discovery |
| `WithEnv(env []string)` | **seed Layer-1 chain result** for interpolation (spec §4 ordering note) |
| `WithProfiles([]string)` / `WithDefaultProfiles(...)` | M3 profiles passthrough |
| `WithResolvedPaths(resolve bool)` | make `EnvFile.Path` **absolute** (we want `true`) |
| `WithInterpolation(bool)` | `${...}` interpolation (keep on — the whole point) |
| `WithDotEnv` / `WithOsEnv` | optional dotenv / OS env merge |
| `WithLoadOptions(...func(*loader.Options))` | reach low-level knobs if needed |

`WithConfigFileEnv` is what makes the loader **include-graph aware** (honors
`COMPOSE_FILE` and resolves `include:`), structurally eliminating the sh kit's
glob over-discovery bug (spec §1, §13.2).

### Layer B — `loader` (low-level, isolate, probably don't need directly)

Package `.../v2/loader`. `LoadWithContext(ctx, types.ConfigDetails,
options ...func(*Options)) (*types.Project, error)` is the core. The `cli` layer
wraps this. Keep direct `loader` use out of `internal/engine`'s public surface;
if we ever need it, reach it via `cli.WithLoadOptions`.

---

## 2. Types we read (verified)

Package `github.com/compose-spec/compose-go/v2/types`.

```
type Project struct {
    WorkingDir       string   // json:"-"
    Services         Services // ACTIVE services (profile-filtered)
    Environment      Mapping  // resolved interpolation env
    DisabledServices Services // services dropped because profile inactive
    ...
}

type Services map[string]ServiceConfig

type ServiceConfig struct {
    ...
    EnvFiles []EnvFile `yaml:"env_file"`
    ...
}

type EnvFile struct {
    Path     string // absolute IF WithResolvedPaths(true)
    Required OptOut // bool whose DEFAULT is true (env_file: required unless "required: false")
    Format   string
}

type OptOut bool // "boolean which default value is 'true'"
```

Key semantics for the engine:

- **`Project.Services` is already the active set.** Profile-inactive services are
  moved to `DisabledServices` by the loader — so iterating `Services` gives us the
  *active* `env_file` set with **no extra filtering** (this is the include-aware,
  no-over-discovery win, spec §13.2).
- **`EnvFile.Path` is absolute** when we pass `WithResolvedPaths(true)`. That
  matches spec §4 step 3 ("resolved absolute paths").
- **`EnvFile.Required`** is `OptOut` (default true). For Layer-2 enumeration we
  emit the path regardless; for a `required:false` file that is *missing* we
  should skip it (mirror sh kit "keep existing files in order"). The loader itself
  errors on a missing **required** env_file — we surface that error.
- Interpolation (`${SVC_DIR}`, nested `${A:-${B:-c}}`) is applied by the loader
  before we read `EnvFiles`, so those resolve for free (spec §13.2 bugs gone).

Helpful `Project` methods (verified) if we need them later:
`WithProfiles([]string)`, `ServiceNames()`, `GetService(name)`,
`AllServices()`. `Services.Filter(pred)` exists for ad-hoc filtering.

---

## 3. Proposed `internal/engine` interface sketch

Isolate ALL compose-go behind this (spec §12 — localize upgrades). The rest of
the CLI never imports compose-go.

```go
// Package engine wraps compose-go (the ONLY package that imports it).
package engine

import "context"

// Input describes one resolution request.
type Input struct {
    ProjectDir string   // working dir (absolute)
    ConfigFiles []string // explicit -f files; empty => COMPOSE_FILE/default discovery
    Env        []string // Layer-1 chain result, "K=V" — seeds interpolation (spec §4 note)
    Profiles   []string // active profiles (M3)
}

// Result is the active Layer-2 env_file set, deduped & ordered, ready to
// append after Layer 1 into COMPOSE_ENV_FILES.
type Result struct {
    EnvFiles []string // absolute paths, active services only, in deterministic order
    Project  ProjectView // minimal read-only view for env-debug/provenance
}

// ProjectView is a compose-go-free projection so internal/debug never imports compose-go.
type ProjectView struct {
    WorkingDir string
    Services   map[string][]string // service -> its resolved env_file abs paths
}

// Engine is the seam. One real impl over compose-go; trivially fakeable in tests.
type Engine interface {
    // Resolve loads the project (include-aware, interpolated) and returns the
    // active env_file set. Returns a wrapped error on load/validation failure.
    Resolve(ctx context.Context, in Input) (Result, error)
}

// New returns the compose-go-backed Engine, pinned to a known compose-go version.
func New() Engine
```

**Mapping to compose-go (real call shape):**

```go
opts, err := cli.NewProjectOptions(in.ConfigFiles,
    cli.WithWorkingDirectory(in.ProjectDir),
    cli.WithConfigFileEnv,           // honor COMPOSE_FILE + include:
    cli.WithDefaultConfigPath,       // default file discovery when none given
    cli.WithEnv(in.Env),             // seed Layer-1 vars for interpolation
    cli.WithProfiles(in.Profiles),
    cli.WithResolvedPaths(true),     // EnvFile.Path => absolute
    cli.WithInterpolation(true),
)
// ...
proj, err := opts.LoadProject(ctx)
// iterate proj.Services (active set) -> svc.EnvFiles[].Path  => Result.EnvFiles
```

Determinism: `Project.Services` is a `map`, so the engine MUST sort (service
name, then file order within a service) before emitting — give qa a stable
contract to assert on.

**Why this seam helps qa (DM'd):** the `Engine` interface + `Input`/`Result`
structs are plain Go with no compose-go types leaking, so qa can:
1. table-drive `Resolve` against `examples/monorepo/` fixtures and assert exact
   `Result.EnvFiles` ordering, and
2. fake the `Engine` to unit-test `chain`/`debug`/`cmd` wiring without docker.

---

## 4. API-stability caveats (spec §12)

1. **compose-go has an evolving API.** Concrete evidence seen this session:
   `ProjectFromOptions` is already **deprecated** in favor of the
   `LoadProject`/`LoadModel` *methods* — the surface shifts release to release.
   → Pin **v2.11.0**; isolate behind `internal/engine` (done in the sketch);
   bump deliberately and re-run acceptance (spec §10). Surface any bump to the
   lead before merging.
2. **`/v2` vs legacy module.** Must import `.../compose-go/v2/...`. The non-v2
   module (v1.20.2) comes in transitively; do not import it.
3. **`EnvFile.Required` default-true (`OptOut`)** is a subtle semantic — a missing
   *required* env_file makes `LoadProject` error. **RULED (D1 §5, provisional):**
   the engine's enumeration pass stays **lenient (skip missing)**; required-fatal
   is left to the real `docker compose` run. See §5 D1 for the full split.
4. **Map iteration order** — `Services` is a Go map; engine MUST sort for
   deterministic `COMPOSE_ENV_FILES`. Contract qa can pin.
5. **`WithResolvedPaths` required for absolute paths** — without it `EnvFile.Path`
   stays relative to its compose file; we rely on absolute for the merged chain.
6. **Version floor** (spec §12) — v2.11.0 targets current Compose features;
   matches the "Compose ≥ 2.24" floor. Confirm at pin time.

---

## 5. Decisions — lead rulings (2026-06-15, provisional; locked at execution)

- **D1 (behavior boundary): RULED — split assembly vs runtime (PROVISIONAL,
  parity-affecting, pending USER confirm).** Our chain-assembly / Layer-2
  enumeration pass stays **lenient: skip a missing `env_file`** (the smoke suite
  relies on missing-file skip) — compose-go's `required:`-fatal must NOT abort
  enumeration. When `docker compose` actually runs, it enforces `required:`
  upstream as normal. So: **lenient at assembly, upstream-faithful at runtime.**
  Engine impl impact: load with required-missing tolerated for enumeration (e.g.
  skip-validation/skip-consistency on the enumeration load, or filter
  `EnvFile.Required` ourselves and drop non-existent paths), and let the real
  `docker compose` exec do the enforcing. ⚠️ Lead will confirm with the user
  before this is locked.
- **D2: RULED — pin exact `v2.11.0`.** Bump deliberately + re-run acceptance
  (upstream-first, spec §10).
- **D3: RULED — yes, compose-go-free `ProjectView`.** `internal/debug` and the
  rest of the CLI never import compose-go; only `internal/engine` does (spec §12
  isolation).

## 6. Reviewer C1 — env_file *path* references a Layer-2 var (resolution model)

Folded in per lead. The gap: an `env_file:` **path** itself can reference a
variable that is only defined by a Layer-2 (service-level) env_file — a
chicken/egg between "which env defines the path" and "which file the path points
to." compose-go interpolates env_file paths using the **load environment** we
seed via `cli.WithEnv` (= the Layer-1 chain result), NOT values discovered inside
other services' Layer-2 env_files. So a path like `env_file: ${SVC_DIR}/.env`
resolves IFF `SVC_DIR` is in Layer 1 (matches spec §4 ordering note); a path that
depends on a var defined only inside another Layer-2 file is **not** resolvable in
a single pass and is out of the single-pass model.

**Needs an explicit resolution model in the spec.** Options for the engine seam:
(a) single-pass, Layer-1-only path interpolation (simplest, upstream-faithful,
documents the limitation — recommended for v1 "thin"); (b) two-pass (enumerate
Layer-2, fold discovered vars back into the load env, re-load) — richer but
diverges from a single `LoadProject` call and risks ordering ambiguity. **Lean
(a) for v1**, capture (b) as deferred. Flagging to the spec owner — this is a
contract decision, not mine to lock.
