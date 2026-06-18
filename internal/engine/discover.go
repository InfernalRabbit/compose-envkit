package engine

import (
	"os"
	"path/filepath"
	"strings"
)

// standardComposeNames matches compose-go's default discovery set.
var standardComposeNames = []string{
	"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml",
}

func seedLookup(env []string, key string) string {
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			return kv[len(key)+1:]
		}
	}
	return ""
}

// interpolateComposeFile substitutes ${CENVKIT_ENV}/${ENV} (and any var present
// in the seed env) into a COMPOSE_FILE entry. This mirrors the semantics
// chain.substituteTokens applies in Layer-1 (replicated here because that helper
// is unexported in package chain), so the gate and the loader interpolate alike.
func interpolateComposeFile(entry string, env []string) string {
	composeEnv := seedLookup(env, "CENVKIT_ENV")
	r := strings.NewReplacer("${CENVKIT_ENV}", composeEnv, "${ENV}", composeEnv)
	return r.Replace(entry)
}

// resolveComposeFiles turns the seed env into an ordered slice of existing
// absolute config paths, the single resolver shared by engine.Resolve (as the
// configs arg when in.ConfigFiles is empty) and the HasComposeFile* gate — so the
// gate and the loader can never disagree.
//
// When COMPOSE_FILE is set it (a) interpolates ${CENVKIT_ENV}/${ENV}; (b) splits
// on COMPOSE_PATH_SEPARATOR-else-os.PathListSeparator (NEVER ',' — probe-verified
// compose-go v2.11.0 never uses ','); (c) joins relative entries to absolute
// against dir (NOT process cwd); (d) keeps only entries that exist on disk.
//
// When COMPOSE_FILE is UNSET it falls back to standard-name discovery in dir and
// returns the existing standard config files. (This standard-name fallback lives
// HERE — rather than only in the gate — so that resolveComposeFiles is itself a
// complete "what config files exist?" answer; the qa contract in discover_test.go
// `TestHasComposeFile` pins `len(resolveComposeFiles(dir, env)) > 0` for a bare
// `compose.yaml` with no COMPOSE_FILE set.)
func resolveComposeFiles(dir string, env []string) []string {
	cf := seedLookup(env, "COMPOSE_FILE")
	if cf == "" {
		var out []string
		for _, n := range standardComposeNames {
			p := filepath.Join(dir, n)
			if _, err := os.Stat(p); err == nil {
				out = append(out, p)
			}
		}
		return out
	}
	sep := seedLookup(env, "COMPOSE_PATH_SEPARATOR")
	if sep == "" {
		sep = string(os.PathListSeparator) // ":" on unix — NEVER ","
	}
	var out []string
	for _, raw := range strings.Split(cf, sep) {
		f := strings.TrimSpace(interpolateComposeFile(raw, env))
		if f == "" {
			continue
		}
		if !filepath.IsAbs(f) {
			f = filepath.Join(dir, f) // resolve against ProjectDir, NOT process cwd
		}
		if _, err := os.Stat(f); err == nil {
			out = append(out, f)
		}
	}
	return out
}

// HasComposeFileEnv is the seam-correct gate: it takes the FULL seed env (the
// chain's Vars) so ${CENVKIT_ENV} interpolation and COMPOSE_PATH_SEPARATOR are
// honored, and shares resolveComposeFiles with Resolve so gate and loader cannot
// drift. When false, callers skip Layer-2 entirely (chain-only mode, spec §13 G4).
func HasComposeFileEnv(dir string, env []string) bool {
	return len(resolveComposeFiles(dir, env)) > 0
}

// HasComposeFile is the single-COMPOSE_FILE-value convenience form (still
// interpolates ${CENVKIT_ENV} when the value carries it AND the caller seeds
// CENVKIT_ENV — prefer HasComposeFileEnv from cmd code, which threads the full env).
func HasComposeFile(dir, composeFileEnv string) bool {
	if composeFileEnv != "" {
		return HasComposeFileEnv(dir, []string{"COMPOSE_FILE=" + composeFileEnv})
	}
	return HasComposeFileEnv(dir, nil)
}
