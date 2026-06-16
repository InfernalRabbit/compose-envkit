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

// Effect is one place a variable's ${VAR} reference took effect in the compose
// model: the service, the dotted+[i] field path, and the resolved string.
type Effect struct {
	Service  string `json:"service"`
	Field    string `json:"field"`
	Resolved string `json:"resolved"`
}

// VarTrace is the full story for one variable: its winning value + source, the
// sources it overrode (A), and where it took effect in the compose model (B-lite).
type VarTrace struct {
	Name       string   `json:"name"`
	Value      string   `json:"value"`
	Winner     Source   `json:"winner"`
	Overridden []Source `json:"overridden,omitempty"`
	Effects    []Effect `json:"effects,omitempty"`
}

// EnvEntry is one key in a service's effective environment with its source (C).
type EnvEntry struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source Source `json:"source"`
}

// ServiceEnv is a service's effective environment with per-key sources (C).
type ServiceEnv struct {
	Service string     `json:"service"`
	Entries []EnvEntry `json:"entries"`
}

// Report is the whole env-debug picture: the ordered COMPOSE_ENV_FILES, the
// per-variable traces (A + B-lite), and the per-service effective env (C, empty
// in chain-only mode).
//
// Files is the FULL merged list (Layer-1 + Layer-2) in COMPOSE_ENV_FILES order;
// ChainFiles is the Layer-1-only subset in chain order. The two are distinct
// views: `--files` renders Files, `--chain` (and the default view) renders
// ChainFiles — v1 semantics where a bare `env-debug` == `--chain` and secrets are
// last WITHIN the Layer-1 chain (acceptance TestScenario12 [12.4]).
type Report struct {
	Files      []string            `json:"files"`              // full merged COMPOSE_ENV_FILES order (Layer-1 + Layer-2)
	ChainFiles []string            `json:"chain_files"`        // Layer-1-only subset, chain order
	Vars       map[string]VarTrace `json:"vars"`               // A + B-lite
	Services   []ServiceEnv        `json:"services,omitempty"` // C (empty in chain-only mode)
}
