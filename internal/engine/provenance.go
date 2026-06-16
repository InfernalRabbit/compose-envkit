package engine

import (
	"context"
	"fmt"
	"sort"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/template"
	"github.com/compose-spec/compose-go/v2/types"

	"github.com/compose-envkit/compose-envkit/internal/provenance"
)

// ProvFile is one ordered COMPOSE_ENV_FILES entry tagged with its chain layer.
type ProvFile struct{ Path, Layer string } // Layer: "layer1" | "layer2"

// ProvInput describes one provenance request: the ordered merged env files (for
// chain attribution A), the chain Vars (interpolation + mapping source), and the
// compose config selection (for per-service env C + ${VAR} effects B-lite).
type ProvInput struct {
	ProjectDir  string
	ConfigFiles []string // explicit -f; empty => resolveComposeFiles
	Env         []string // chain Vars "K=V" (interpolation + mapping source)
	Profiles    []string
	EnvFiles    []ProvFile // ordered merged COMPOSE_ENV_FILES (for A attribution)
}

// parseDotEnv uses compose-go's own dotenv parser (docker-compose parity).
// lookup lets in-file ${VAR} refs that the chain provides resolve quietly
// (mirrors the WithoutLogging discipline of the B-lite Substitute path). A
// genuinely-unset external ${VAR} inside an env_file STILL warns — that is
// correct docker-compose parity (last-wins COMPOSE_ENV_FILES); only
// chain-provided vars are silenced.
func parseDotEnv(path string, lookup dotenv.LookupFn) (map[string]string, error) {
	return dotenv.ReadFile(path, lookup)
}

func envSliceToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if i := indexByte(kv, '='); i > 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// Provenance assembles A (chain attribution), B-lite (${VAR} effects) and C
// (per-service env with sources) into a single provenance.Report, fully in
// process (no docker daemon). compose-go work is confined to this package.
func (e *composeEngine) Provenance(ctx context.Context, in ProvInput) (provenance.Report, error) {
	configs := in.ConfigFiles
	if len(configs) == 0 {
		configs = resolveComposeFiles(in.ProjectDir, in.Env)
	}

	rep := provenance.Report{Vars: map[string]provenance.VarTrace{}}

	// --- merged interpolation env (mirror docker compose: read COMPOSE_ENV_FILES
	// before interpolation). Build chainEnv = union of every EnvFiles entry in
	// declaration order (later file wins, so Layer-2 over Layer-1), THEN overlay
	// in.Env (OS / Layer-1 chain seed) LAST so the OS/shell value wins — parity
	// with chain.go's OS-wins merge. We capture each file's parsed map once here
	// and REUSE it for the A-attribution loop below (no double-parse).
	chainEnv := map[string]string{}
	parsed := make([]map[string]string, len(in.EnvFiles)) // parsed[i] aligns with in.EnvFiles[i]
	lookup := dotenv.LookupFn(func(k string) (string, bool) { v, ok := chainEnv[k]; return v, ok })
	for i, f := range in.EnvFiles {
		// First pass without a full lookup (lookup is built incrementally); refs
		// that resolve later still win via the overlay. Practical chains rarely
		// depend on cross-file ${VAR} inside an env_file; the C/B-lite paths
		// reparse with the fully-populated lookup below.
		if m, err := parseDotEnv(f.Path, lookup); err == nil {
			parsed[i] = m
			for k, v := range m {
				chainEnv[k] = v // later file in declaration order wins
			}
		}
	}
	for k, v := range envSliceToMap(in.Env) {
		chainEnv[k] = v // OS / Layer-1 seed wins last
	}
	// mapping (B-lite) reads the merged effective env so "${WEB_PORT:-0}:80"
	// resolves to the real value (e.g. "8080:80"), not the :-0 default.
	mapping := template.Mapping(func(k string) (string, bool) { v, ok := chainEnv[k]; return v, ok })

	// --- A: chain attribution over the ordered merged COMPOSE_ENV_FILES ---
	// rep.Files is the full merged list (all layers); rep.ChainFiles is the
	// Layer-1-only subset in chain order. env-debug --chain (and the bare default
	// view) renders ChainFiles so secrets stay last WITHIN the Layer-1 chain
	// (acceptance TestScenario12 [12.4]). Both appends happen before the missing-file
	// continue so the file listings stay consistent, and before the chain-only
	// early-return below so ChainFiles is populated in chain-only mode too.
	for i, f := range in.EnvFiles {
		rep.Files = append(rep.Files, f.Path)
		if f.Layer == "layer1" {
			rep.ChainFiles = append(rep.ChainFiles, f.Path)
		}
		m := parsed[i]
		if m == nil {
			continue // skip missing (parity)
		}
		src := provenance.Source{File: f.Path, Layer: f.Layer}
		for k, v := range m {
			vt := rep.Vars[k]
			vt.Name = k
			if vt.Winner.File != "" {
				vt.Overridden = append(vt.Overridden, vt.Winner)
			}
			vt.Winner, vt.Value = src, v
			rep.Vars[k] = vt
		}
	}

	// --- A overlay: shell environment is the FINAL last-wins source, so each
	// VarTrace's Winner/Value/Overridden stay mutually consistent with what
	// compose actually interpolates (chainEnv overlays files with OS env). When
	// the env overrides a file, the file becomes Overridden and the winner
	// becomes (environment); a shell-only var (no chain file set it) is attributed
	// to (environment) too.
	envSrc := provenance.Source{File: "(environment)", Layer: "environment"}
	for _, k := range sortedKeys(chainEnv) {
		v := chainEnv[k]
		vt := rep.Vars[k]
		vt.Name = k
		if vt.Winner.File != "" && vt.Value != v {
			// a file set it but the shell overrides → shadow the file winner
			vt.Overridden = append(vt.Overridden, vt.Winner)
			vt.Winner, vt.Value = envSrc, v
		} else if vt.Winner.File == "" {
			// shell-only var (no chain file set it)
			vt.Winner, vt.Value = envSrc, v
		}
		rep.Vars[k] = vt
	}

	// chain-only mode: no compose file => return A only.
	if len(configs) == 0 {
		return rep, nil
	}

	// --- C: per-service env with sources (D1 lever keeps environment inline-only) ---
	// Feed the SAME merged env (sorted "K=V") to cli.WithEnv so inline
	// environment values like "${WEB_PORT:-0}" resolve to the real chain value,
	// consistent with the B-lite mapping above.
	mergedEnv := make([]string, 0, len(chainEnv))
	for _, k := range sortedKeys(chainEnv) {
		mergedEnv = append(mergedEnv, k+"="+chainEnv[k])
	}
	opts, err := cli.NewProjectOptions(configs,
		cli.WithWorkingDirectory(in.ProjectDir),
		cli.WithEnv(mergedEnv),
		cli.WithProfiles(in.Profiles),
		cli.WithResolvedPaths(true),
		cli.WithInterpolation(true),
		cli.WithoutEnvironmentResolution, // REQUIRED for C separability (probe §6)
	)
	if err != nil {
		return provenance.Report{}, fmt.Errorf("provenance options: %w", err)
	}
	proj, err := opts.LoadProject(ctx)
	if err != nil {
		return provenance.Report{}, fmt.Errorf("provenance load: %w", err)
	}
	svcNames := make([]string, 0, len(proj.Services))
	for n := range proj.Services {
		svcNames = append(svcNames, n)
	}
	sort.Strings(svcNames)
	for _, name := range svcNames {
		svc := proj.Services[name]
		final := map[string]string{}
		source := map[string]provenance.Source{}
		for _, ef := range svc.EnvFiles {
			m, perr := parseDotEnv(ef.Path, lookup)
			if perr != nil {
				continue
			}
			for k, v := range m {
				final[k] = v
				source[k] = provenance.Source{File: ef.Path, Layer: "env_file"}
			}
		}
		for k, vp := range svc.Environment { // map[string]*string; inline overrides
			val := ""
			if vp != nil {
				val = *vp
			}
			final[k] = val
			source[k] = provenance.Source{File: "(inline environment:)", Layer: "environment"}
		}
		se := provenance.ServiceEnv{Service: name}
		for _, k := range sortedKeys(final) {
			se.Entries = append(se.Entries, provenance.EnvEntry{Key: k, Value: final[k], Source: source[k]})
		}
		rep.Services = append(rep.Services, se)
	}

	// --- B-lite: single RAW load + dict walk + per-leaf Substitute ---
	details, err := loader.LoadConfigFiles(ctx, configs, in.ProjectDir)
	if err != nil {
		return provenance.Report{}, fmt.Errorf("provenance raw config: %w", err)
	}
	details.Environment = types.Mapping(chainEnv)
	rawDict, err := loader.LoadModelWithContext(ctx, *details, func(o *loader.Options) {
		o.SkipInterpolation = true
		o.SkipValidation = true
		o.SkipConsistencyCheck = true
		o.SkipResolveEnvironment = true
	})
	if err != nil {
		return provenance.Report{}, fmt.Errorf("provenance raw load: %w", err)
	}
	walkDict(rawDict, "", func(path, leaf string) {
		vars := template.ExtractVariables(map[string]any{"x": leaf}, template.DefaultPattern)
		if len(vars) == 0 {
			return
		}
		svc, field, ok := splitServiceField(path)
		if !ok {
			return
		}
		resolved, _ := template.SubstituteWithOptions(leaf, mapping, template.WithoutLogging)
		for vn := range vars {
			vt := rep.Vars[vn]
			vt.Name = vn
			vt.Effects = append(vt.Effects, provenance.Effect{Service: svc, Field: field, Resolved: resolved})
			rep.Vars[vn] = vt
		}
	})
	for k, vt := range rep.Vars { // deterministic effects order
		sort.Slice(vt.Effects, func(i, j int) bool {
			if vt.Effects[i].Service != vt.Effects[j].Service {
				return vt.Effects[i].Service < vt.Effects[j].Service
			}
			return vt.Effects[i].Field < vt.Effects[j].Field
		})
		rep.Vars[k] = vt
	}
	return rep, nil
}

func sortedKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// walkDict recurses map[string]any / []any and calls fn on every string leaf
// with a dotted+[i] path (e.g. "services.web.ports[0]"). Map keys are sorted for
// deterministic traversal.
func walkDict(node any, path string, fn func(path, leaf string)) {
	switch v := node.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p := k
			if path != "" {
				p = path + "." + k
			}
			walkDict(v[k], p, fn)
		}
	case []any:
		for i, child := range v {
			walkDict(child, fmt.Sprintf("%s[%d]", path, i), fn)
		}
	case string:
		fn(path, v)
	}
}

// splitServiceField turns "services.web.ports[0]" into ("web", "ports[0]", true).
// It INTENTIONALLY returns ok=false for non-service paths (top-level
// networks/volumes/configs/secrets/x-*); those ${VAR} effects are out of scope
// for B-lite (see spec §2/§8) and are deliberately dropped, not a bug. Such a
// var still appears in A (chain attribution) if it is a COMPOSE_ENV_FILES key;
// only its Effects entry is omitted.
func splitServiceField(path string) (service, field string, ok bool) {
	const p = "services."
	if len(path) <= len(p) || path[:len(p)] != p {
		return "", "", false
	}
	rest := path[len(p):]
	i := indexByte(rest, '.')
	if i < 0 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}
