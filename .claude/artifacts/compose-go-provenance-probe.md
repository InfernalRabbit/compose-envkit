# compose-go v2.11.0 provenance probe (B-lite + A + C)

Method: live throwaway module `/tmp/cenvkit-prov-probe/`, `go run` against the
EXACT pinned module + `go doc` on the real package source. (context7/serena were
not needed; the live-program + `go doc` probe IS the verification method per the
MCP-unreachable fallback.) go1.26.3.

## 1. Pin proof

```
$ go list -m github.com/compose-spec/compose-go/v2
github.com/compose-spec/compose-go/v2 v2.11.0
```

## 2. RECOMMENDED B-lite mechanism (single raw load + per-leaf Substitute)

The naive plan (two dicts: walk load₂ for paths, read the SAME path from an
interpolated load₁) is **inferior** because interpolation expands short forms.
Proven: with `WEB_PORT=8080`, the interpolated `services.web.ports[0]` is NOT the
string `"8080:80"` — it is a normalized map `{mode,protocol,published,target}`.
This happens during **interpolation**, independent of normalization
(`SkipNormalization` does not prevent it — see §3 below). Reading load₁ at
`ports[0]` therefore yields a struct, not the tidy resolved value.

**Recommended:** ONE non-interpolated (raw) dict, then resolve each `${VAR}`-
bearing string leaf in place with `template.SubstituteWithOptions(leaf, mapping,
template.WithoutLogging)`. Clean resolved string, exact field, no daemon, no
normalization surprise, one load instead of two.

Exact code (the snippet I ran, trimmed):

```go
// load the RAW (non-interpolated) merged dict
details, _ := loader.LoadConfigFiles(ctx, configFiles, workingDir) // env not an arg here
details.Environment = types.Mapping{ /* the chain env, key=value */ }
rawDict, _ := loader.LoadModelWithContext(ctx, *details, func(o *loader.Options) {
    o.SkipInterpolation     = true   // ${VAR} literals survive
    o.SkipValidation        = true
    o.SkipConsistencyCheck  = true
    o.SkipResolveEnvironment = true
})

mapping := template.Mapping(func(k string) (string, bool) {
    v, ok := chainEnv[k]; return v, ok          // chainEnv = COMPOSE_ENV_FILES merged map
})

// walk the dict; at each string leaf, find refs (names) and resolve the leaf
walk(rawDict, "", func(path, leaf string) {
    names := keysOf(template.ExtractVariables(map[string]any{"x": leaf}, template.DefaultPattern))
    if len(names) == 0 { return }
    resolved, _ := template.SubstituteWithOptions(leaf, mapping, template.WithoutLogging)
    svc, field := splitServiceField(path)   // "services.web.ports[0]" -> ("web","ports[0]")
    for _, n := range names {
        emit(EffectFact{Service: svc, Field: field, Resolved: resolved}, varName=n)
    }
})
```

`walk` recurses `map[string]any` / `[]any`, builds dotted+`[i]` paths, calls back
on string leaves. The `pattern` arg to `ExtractVariables` is `template.DefaultPattern`
(do NOT pass `nil`; see §4). `template.Substitute` == `SubstituteWithOptions`
with no options — use the Options form to attach `WithoutLogging`.

> Note: `loader.LoadConfigFiles` does NOT take environment; it builds
> `*types.ConfigDetails` with empty `Environment`. You must set
> `details.Environment` before `LoadModelWithContext` (used for include/extends
> path interpolation), and pass the same map as the `template.Mapping` for leaf
> resolution.

## 3. Empirical output

Fixture `web/compose.yaml`: `web{image,ports:["${WEB_PORT}:80"],environment:{TIER:"${COMPOSE_ENV}",STATIC:hello},env_file:[common.env,web.env]}`,
`db{environment:["PGDATA=/data/${DATA_DIR}"]}`; env `WEB_PORT=8080 COMPOSE_ENV=staging DATA_DIR=pgdata`.

B-lite (recommended per-leaf-Substitute approach):
```
  ${WEB_PORT}    @ service=web  field=ports[0]            resolved="8080:80"
  ${COMPOSE_ENV} @ service=web  field=environment.TIER    resolved="staging"
  ${DATA_DIR}    @ service=db   field=environment[0]      resolved="PGDATA=/data/pgdata"
```

(For contrast, the two-dict `getPath` approach returned for the same WEB_PORT ref:
`resolved="map[mode:ingress protocol:tcp published:8080 target:80]"` — the
normalization trap. Use per-leaf Substitute.)

Per-service env with source (C):
```
  service db:
    PGDATA   = "/data/pgdata"   <- inline environment:
  service web:
    SHARED   = "base"           <- env_file:common.env
    STATIC   = "hello"          <- inline environment:
    TIER     = "staging"        <- inline environment:   (overrides env_file TIER)
    WEB_ONLY = "yes"            <- env_file:web.env
```

dotenv.Parse semantics (fixture with comment/export/quotes/escape/dup/in-file-interp):
```
  DUP      = "second"            (duplicate key -> last wins)
  EMPTY    = ""
  ESCAPED  = "line1\nline2"      (double-quote escapes processed)
  EXPORTED = "exp-value"         (export prefix stripped)
  INTERP   = "exp-value-suffix"  (in-file ${EXPORTED} interpolation)
  QUOTED   = "hello world"
  SINGLE   = "no $interp here"   (single quotes = literal, no interp)
  (comment line ignored)
```

Substitute edge cases:
```
  "${SET}"                 -> "yes"
  "${UNSET:-fallback}"     -> "fallback"
  "${UNSET-fallback}"      -> "fallback"
  "prefix-${SET}-${UNSET:-d}" -> "prefix-yes-d"
  "${UNSET}"               -> ""     (+ stderr WARNING unless WithoutLogging)
  "literal$$dollar"        -> "literal$dollar"  ($$ escape)
  "${SET:+present}"        -> "present"          (:+ presence value)
```
ExtractVariables defaults/required: `${FOO:-dv}` -> default="dv";
`${BAR:?must}` -> required=true; `${BAZ}` -> required=false. So `Variable`
carries `{Name, DefaultValue, PresenceValue, Required}` but **no path**.

## 4. go doc evidence (key symbols)

```
// dotenv
func Parse(r io.Reader) (map[string]string, error)
func ParseWithLookup(r io.Reader, lookupFn LookupFn) (map[string]string, error)
func ReadFile(filename string, lookupFn LookupFn) (map[string]string, error)  // path convenience
type LookupFn func(string) (string, bool)

// template
var DefaultPattern = regexp.MustCompile(patternString)   // the ${...} regexp
func ExtractVariables(configDict map[string]interface{}, pattern *regexp.Regexp) map[string]Variable
    // "returns a map of all the variables defined ... and their default value if any."
type Variable struct { Name string; DefaultValue string; PresenceValue string; Required bool }
func Substitute(template string, mapping Mapping) (string, error)
func SubstituteWithOptions(template string, mapping Mapping, options ...Option) (string, error)
func WithoutLogging(cfg *Config)            // pass as an Option to silence unset-var warning
type Mapping func(string) (string, bool)

// loader (the dict path)
func LoadConfigFiles(ctx, configFiles []string, workingDir string, options ...func(*Options)) (*types.ConfigDetails, error)
func LoadModelWithContext(ctx, configDetails types.ConfigDetails, options ...func(*Options)) (map[string]any, error)
    // "returns a fully loaded configuration as a yaml dictionary"
type Options struct { SkipInterpolation, SkipValidation, SkipNormalization,
    SkipConsistencyCheck, SkipResolveEnvironment bool; ... }
```

`DefaultPattern` is a package var (`regexp.MustCompile(patternString)`) — pass it
explicitly to `ExtractVariables`; the package does not default a nil pattern there.

## 5. Does ExtractVariables give field paths? NO — walk the dict.

Proven: `ExtractVariables(fullDict, DefaultPattern)` returns
`map[string]Variable` **keyed by variable name**, with NO service/field
coordinate. Output for the fixture was exactly `{COMPOSE_ENV, DATA_DIR, WEB_PORT}`
(names only). Therefore the engine MUST walk the dict itself to recover paths
(the recommended mechanism in §2). `ExtractVariables` is still useful per-leaf
(feed a 1-key map) to (a) detect whether a leaf references any var and (b) read
defaults/required — but the path comes from the walk, not from the function.

## 6. Per-service env (C): separating env_file from inline environment

THE GOTCHA (proven, the load-flag that makes C possible):

| `WithoutEnvironmentResolution` | `svc.Environment` (web) |
|---|---|
| OFF (default)                  | `[SHARED STATIC TIER WEB_ONLY]` — **merged with env_file, not separable** |
| ON (the v1 D1 lever)           | `[STATIC TIER]` — **inline-only; env_file kept in `svc.EnvFiles`** |

The engine ALREADY loads with `cli.WithoutEnvironmentResolution` (engine.go:66,
the D1 missing-file lever), so `ServiceConfig.Environment` is the **inline-only**
mapping and `svc.EnvFiles[].Path` is the ordered resolved env_file list. C is then:
1. for each `ef` in `svc.EnvFiles` (in order): `dotenv.ReadFile(ef.Path, nil)`;
   set `final[k]=v, source[k]=env_file:<base>` (later env_file overrides earlier).
2. apply inline `svc.Environment` (override; source = `inline environment:`).
This produced the correct attribution in §3 (TIER from inline overrode the
env_file TIER; SHARED/WEB_ONLY attributed to their files). If a future change
drops the lever, C breaks — keep `WithoutEnvironmentResolution` for the
provenance load.

`svc.Environment` is `map[string]*string` (nil pointer = key with no value;
treat as "" when present).

## 7. Gotchas / caveats for the engine impl

1. **Normalization-on-interpolation trap.** Do NOT map a ref's path into an
   interpolated dict/Project expecting a clean string — interpolation expands
   short forms (`ports`, list `environment`) into structs/`KEY=VALUE`. Resolve the
   RAW leaf string with `template.SubstituteWithOptions` instead. (One raw load;
   no second interpolated load needed for B-lite.)
2. **Unset-var stderr warning.** `template.Substitute` logs
   `level=warning ... not set. Defaulting to a blank string.` to stderr for a
   `${UNSET}` with no default. Use `SubstituteWithOptions(..., template.WithoutLogging)`
   so cenvkit doesn't leak warnings. (Confirmed silenced.)
3. **`LoadConfigFiles` ignores env** — set `details.Environment` manually before
   `LoadModelWithContext`; pass the same map as the `template.Mapping`.
4. **Pass `template.DefaultPattern`** to `ExtractVariables` (not nil).
5. **Keep `WithoutEnvironmentResolution`** on the provenance load or C cannot
   separate env_file from inline (§6).
6. **env_file order within a service is significant** (later overrides earlier);
   `svc.EnvFiles` preserves declaration order. Use `dotenv.ReadFile`/`Parse`
   (parity) not a hand-rolled parser.
7. **`svc.Environment` value is `*string`** (nil-safe).
8. The dict walk handles `map[string]any` + `[]any` + string leaves; non-string
   scalars (already-typed ints/bools) never carry `${VAR}` in the raw dict, so the
   string-leaf-only walk is sufficient.
9. **module hygiene:** `dotenv`, `template`, `loader` are all sub-packages of the
   already-pinned `compose-go/v2` — no new dependency, no version drift. They must
   live behind `internal/engine` (CI seam: only engine imports compose-go).
10. Mechanism is fully **daemon-free** (no docker), as required.
