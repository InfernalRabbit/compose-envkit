// Package provenance is the pure-Go leaf that owns the env-debug data model and
// its human/JSON rendering. It imports NEITHER compose-go NOR internal/engine —
// engine imports IT for the shared types, so this package stays a fast,
// dependency-light leaf (the CI seam check guards that compose-go never leaks in).
package provenance

// Source identifies where a value came from: a concrete file (or a synthetic
// "(environment)" / "(inline environment:)" marker) and which layer set it.
type Source struct {
	File  string `json:"file"`
	Layer string `json:"layer"` // layer1 | layer2 | env_file | environment
}

// ServiceVal is one service `env_file:` definition of a variable: the service it
// is declared under, the env_file path, and the value. It is gap EVIDENCE —
// runtime-only (per the service's container), NOT part of the interpolation env.
type ServiceVal struct {
	Service string `json:"service"`
	File    string `json:"file"`
	Value   string `json:"value"`
}

// Effect is one place a variable's ${VAR} reference took effect in the compose
// model: the service, the dotted+[i] field path, and the resolved string.
//
// Resolved is the REAL run value — the value the live `docker compose` run would
// interpolate against the Layer-1-only env. A var defined ONLY in a service
// env_file: resolves to its `:-default`/empty fallback here (Gap=true), because
// service env_files are runtime-only and never feed interpolation (v3, #3435).
type Effect struct {
	Service  string `json:"service"`
	Field    string `json:"field"`
	Resolved string `json:"resolved"`
	Gap      bool   `json:"gap"` // ${VAR} is env_file-only -> falls back at the run
}

// VarTrace is the full story for one variable: its winning value + source, the
// sources it overrode (A), where it took effect in the compose model (B-lite),
// and — post-v3 — whether it is a runtime-vs-interpolation gap.
//
// Value/Winner/Overridden attribute over the INTERPOLATION env only (Layer-1
// chain + shell overlay). A var defined only in a service env_file: has no chain
// winner (empty Winner) and InChain=false; its env_file definitions live in
// RuntimeDefs as gap evidence.
type VarTrace struct {
	Name        string       `json:"name"`
	Value       string       `json:"value"`
	Winner      Source       `json:"winner"`
	Overridden  []Source     `json:"overridden,omitempty"`
	Effects     []Effect     `json:"effects,omitempty"`
	InChain     bool         `json:"in_chain"`               // resolvable from the interpolation (Layer-1 + shell) env?
	RuntimeDefs []ServiceVal `json:"runtime_defs,omitempty"` // service env_file: defs (runtime-only; gap evidence)
	Gap         bool         `json:"gap"`                    // referenced && !InChain && len(RuntimeDefs)>0
}

// EnvEntry is one key in a service's effective environment with its source (C).
type EnvEntry struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source Source `json:"source"`
}

// ServiceEnv is a service's effective environment with per-key sources (C).
//
// EnvFiles is the service's DECLARED `env_file:` paths in declared order — the
// runtime-only set the `--files` two-group view renders. It is kept separate from
// Entries because Entries' per-key Source can be rewritten to "(inline
// environment:)" when an inline `environment:` key shadows an env_file key; if
// EVERY key of a file is overridden, that file would vanish from an
// entries-derived list. Declaring the paths here keeps `--files` faithful to what
// the service actually loads at runtime, regardless of inline overrides.
type ServiceEnv struct {
	Service  string     `json:"service"`
	Entries  []EnvEntry `json:"entries"`
	EnvFiles []string   `json:"env_files,omitempty"` // declared env_file: paths, declared order (runtime-only; for --files)
}

// Report is the whole env-debug picture: the ordered COMPOSE_ENV_FILES, the
// per-variable traces (A + B-lite), and the per-service effective env (C, empty
// in chain-only mode).
//
// Post-v3 (Layer-2 debug-only): Files is the new COMPOSE_ENV_FILES = Layer-1
// ONLY, so Files == ChainFiles by construction. Both fields are retained (D2):
// the runtime-only Layer-2 set the `--files` two-group view shows comes from
// Services (service `env_file:` paths), NOT from Files. `--chain` (and the bare
// default view) renders ChainFiles — secrets stay last WITHIN the Layer-1 chain
// (acceptance TestScenario12 [12.4]).
type Report struct {
	Files      []string            `json:"files"`              // new COMPOSE_ENV_FILES order (Layer-1 ONLY; == ChainFiles)
	ChainFiles []string            `json:"chain_files"`        // Layer-1 chain order (kept for the --chain path; D2)
	Vars       map[string]VarTrace `json:"vars"`               // A + B-lite + gap (InChain/RuntimeDefs/Gap)
	Services   []ServiceEnv        `json:"services,omitempty"` // C (also the runtime-only group for --files)
}
