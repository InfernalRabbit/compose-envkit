// Package envmap flattens a resolved Layer-1 chain into a merged KEY=VALUE
// environment for the populator (cenvkit run / cenvkit env). It is the no-docker
// "local arm" of the same chain: pure Go, importing internal/engine for the ONE
// shared expansion primitive (engine.Flatten) so run/env, env-debug and
// `docker compose config` all resolve ${VAR} identically (spec §5c, MF4).
//
// envmap itself imports NO compose-go — the seam (only internal/engine touches
// compose-go) is preserved. It owns the shell-wins overlay and the output
// formatting (the parts that are pure string handling).
package envmap

import (
	"sort"

	"github.com/InfernalRabbit/compose-envkit/internal/engine"
)

// Resolved is the flattened chain environment. Full is the complete process env
// with the chain values overlaid SHELL-WINS (what `cenvkit run` execs with).
// ChainKeys is the sorted set of keys the chain itself contributed — `cenvkit
// env` emits ONLY these (bounded, reproducible: it must not dump the whole
// inherited process env into CI logs, spec §5e). The emitted value for a chain
// key is Full[k], so a shell override of a chain key is reflected.
type Resolved struct {
	Full      map[string]string // process env + chain (shell-wins); run execs with this
	ChainKeys []string          // sorted keys the chain contributed; env emits only these
}

// Resolve flattens the existence-filtered chain file list into Resolved.
//
//   - expand=true  → ${VAR}/${VAR:-default} expand via engine.Flatten
//     (dotenv.GetEnvFromFile); unset ${VAR} with no default → empty (compose
//     parity), not an error.
//   - expand=false → literal values, ${...} unexpanded (engine.ParseOrderedLiteral
//     under the hood).
//
// Shell-wins applies IDENTICALLY on both paths: --no-expand suppresses only the
// ${VAR} expansion, never the overlay (spec §5b). files MUST be existence-filtered
// (chain.Resolve drops missing); a vanished file (TOCTOU) surfaces as Flatten's
// error and is fatal to the caller.
func Resolve(processEnv map[string]string, files []string, expand bool) (Resolved, error) {
	chainVals, err := engine.Flatten(processEnv, files, expand)
	if err != nil {
		return Resolved{}, err
	}
	// Shell-wins overlay: clone the process env, then add a chain key only if the
	// shell did NOT already set it (mirrors compose's cli.WithDotEnv → Mapping.Merge
	// add-only-unset, types/mapping.go:183).
	full := make(map[string]string, len(processEnv)+len(chainVals))
	for k, v := range processEnv {
		full[k] = v
	}
	for k, v := range chainVals {
		if _, set := full[k]; !set {
			full[k] = v
		}
	}
	keys := make([]string, 0, len(chainVals))
	for k := range chainVals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return Resolved{Full: full, ChainKeys: keys}, nil
}
