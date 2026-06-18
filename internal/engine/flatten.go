package engine

import (
	"github.com/compose-spec/compose-go/v2/dotenv"
)

// Flatten resolves an ordered Layer-1 file list into a merged KEY=VALUE map,
// last-wins across files. It is the SINGLE expansion primitive (spec §5c, MF4):
// env-debug's interpolation env and the populator (cenvkit run/env) both route
// through it, so the same chain yields identical ${VAR} values everywhere.
//
// It returns ONLY the parsed FILE values — base is consulted SOLELY as a ${VAR}
// lookup source (matching dotenv.GetEnvFromFile, which exposes the full base to
// every file immediately), and is NOT copied into the result. The caller owns the
// final overlay: e.g. the populator's shell-wins merge (internal/envmap) and
// env-debug's interpEnv overlay both add base/shell keys themselves. Keeping base
// out of the result is what makes `cenvkit env` emit only chain-derived keys.
//
//   - expand=true  → dotenv.GetEnvFromFile(base, files): in-file ${VAR}/${VAR:-def}
//     resolve against base + already-accumulated chain values, exactly as
//     `docker compose` reads its env files (compose-go's own primitive).
//   - expand=false → per-file ParseOrderedLiteral folded last-wins: values verbatim,
//     ${...} left UNexpanded (no third literal reader — reuses the --overview one).
//
// files MUST contain only existing paths: GetEnvFromFile ERRORS (does not skip) on
// a missing file (dotenv/env.go:36), so callers feed it chain.Resolve's
// existence-filtered list only (MF2). base may be nil.
func Flatten(base map[string]string, files []string, expand bool) (map[string]string, error) {
	if expand {
		if base == nil {
			base = map[string]string{}
		}
		return dotenv.GetEnvFromFile(base, files)
	}
	// --no-expand: literal values, last-wins across files, ${...} unexpanded.
	out := map[string]string{}
	for _, f := range files {
		entries, err := ParseOrderedLiteral(f)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			out[e.Key] = e.RawValue // later file in chain order wins
		}
	}
	return out, nil
}
