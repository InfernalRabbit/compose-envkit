package engine

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/template"
	"github.com/compose-spec/compose-go/v2/types"

	"github.com/InfernalRabbit/compose-envkit/internal/provenance"
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
	WantLayers  bool       // populate Report.Layers (the raw ordered --overview lens); off by default to keep other modes' JSON unchanged (D-A)
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

// ParseOrderedLiteral reads a dotenv-style file into ORDERED, LITERAL entries for
// the --overview lens AND the populator's --no-expand path (internal/envmap folds
// the ordered entries into a last-wins map). compose-go's dotenv parser cannot be
// reused here: it returns an unordered map AND expands ${...} (verified v2.11.0,
// plan §0). This is a thin stdlib-only line reader (keeps the engine seam — no
// extra compose-go surface). It mirrors dotenv's KEY tokenization (skip
// blank/#-comment lines; strip a leading `export `; split on the first `=`; key
// charset [A-Za-z0-9_.-]) but takes the VALUE verbatim — NO ${...} expansion and NO
// escape processing — except it strips one matching surrounding quote pair, and for
// an UNQUOTED value trims a trailing " # comment" + trailing space (mirrors dotenv
// parser.go:157-159; decision D-B).
func ParseOrderedLiteral(path string) ([]provenance.OverviewEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var entries []provenance.OverviewEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// strip a leading `export` + following whitespace (mirror dotenv ^export\s+:
		// tabs too, N-1) — only when a space/tab actually follows, never "exportFOO".
		if rest, ok := strings.CutPrefix(line, "export"); ok && rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
			line = strings.TrimLeft(rest, " \t")
		}
		i := strings.IndexByte(line, '=')
		if i <= 0 {
			continue // no key, or "=value" — skip (parity with a missing key name)
		}
		key := strings.TrimSpace(line[:i])
		if key == "" || !validEnvKey(key) {
			continue
		}
		raw := line[i+1:]                   // value as-read, leading space intact (dotenv-parity for the # cut)
		val := strings.TrimLeft(raw, " \t") // for quote detection
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1] // quoted: strip ONE matching pair, keep contents verbatim
		} else {
			// unquoted: cut an inline " # comment" on the RAW slice (so a value that
			// is ONLY a trailing comment, e.g. `KEY= # x`, becomes "" — SF-3), then
			// trim surrounding whitespace.
			if j := strings.Index(raw, " #"); j >= 0 {
				raw = raw[:j]
			}
			val = strings.TrimSpace(raw)
		}
		entries = append(entries, provenance.OverviewEntry{Key: key, RawValue: val})
	}
	return entries, sc.Err()
}

// validEnvKey reports whether key matches dotenv's variable-name charset
// [A-Za-z0-9_.-] (mirrors parser.go:122-127). Rejects whitespace/`$`/etc., so a
// malformed line is skipped rather than rendered as a bogus entry.
func validEnvKey(key string) bool {
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '_', r == '.', r == '-':
		default:
			return false
		}
	}
	return true
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

	// --- two env contexts (v3: Layer-2 is debug-only) ---
	// chainEnv  = union of EVERY EnvFiles entry (all layers) + in.Env last. Its
	//             ONLY remaining role is the dotenv lookup below — so an in-file
	//             ${VAR} reference inside an env_file can resolve against the full
	//             union (parity with how compose reads dotenv files). It does NOT
	//             feed interpolation: neither the C-load WithEnv nor the B-lite
	//             mapping read it (both use interpEnv — see the lead's v3
	//             correction: --effective must not report a value the run won't).
	// interpEnv = Layer-1 files + shell (in.Env) ONLY — the REAL run
	//             interpolation env (the new COMPOSE_ENV_FILES). The B-lite
	//             mapping, A-attribution, the A overlay, and the C-load WithEnv all
	//             read THIS, so a ${VAR} defined only in a service env_file:
	//             resolves to its :-default fallback, exactly like the live
	//             `docker compose` run (#3435: env_file is not interpolated).
	// We capture each file's parsed map once and REUSE it for A-attribution below.
	//
	// U1 (spec §5c, MF4): interpEnv is built via engine.Flatten (= dotenv.
	// GetEnvFromFile) so env-debug and the populator (cenvkit run/env) share ONE
	// expansion primitive — the same chain yields IDENTICAL ${VAR} values across
	// commands. Flatten exposes the full shell base to every file immediately
	// (compose parity), unlike the incremental chainEnv lookup below whose shell
	// overlay lands only AFTER the file loop. chainEnv stays incremental on purpose:
	// its ONLY role is the dotenv lookup, which is not parity-critical (the two-env
	// split is preserved — do NOT collapse them).
	shellMap := envSliceToMap(in.Env)
	chainEnv := map[string]string{}
	parsed := make([]map[string]string, len(in.EnvFiles)) // parsed[i] aligns with in.EnvFiles[i]
	lookup := dotenv.LookupFn(func(k string) (string, bool) { v, ok := chainEnv[k]; return v, ok })
	var layer1Files []string
	for i, f := range in.EnvFiles {
		if f.Layer == "layer1" {
			layer1Files = append(layer1Files, f.Path)
		}
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
	// interpEnv = Flatten(shell, Layer-1 files) then shell overlaid last (shell wins),
	// matching the run's COMPOSE_ENV_FILES interpolation. Flatten returns file values
	// only; in.EnvFiles already carries existence-filtered paths (chain.Resolve drops
	// missing), so GetEnvFromFile's error-on-missing never fires here.
	interpEnv, err := Flatten(shellMap, layer1Files, true)
	if err != nil {
		return provenance.Report{}, fmt.Errorf("provenance flatten layer-1: %w", err)
	}
	for k, v := range shellMap {
		chainEnv[k] = v  // OS / Layer-1 seed wins last (dotenv-lookup role)
		interpEnv[k] = v // shell overlays the Layer-1 interpolation env (shell wins)
	}
	// mapping (B-lite) reads the Layer-1-only interpolation env, so "${WEB_PORT:-0}:80"
	// resolves to the REAL run value: the chain value when WEB_PORT is in Layer-1,
	// else the :-0 fallback when it is defined only in a service env_file (the gap).
	mapping := template.Mapping(func(k string) (string, bool) { v, ok := interpEnv[k]; return v, ok })

	// --- A: chain attribution over the interpolation (Layer-1) chain ---
	// Post-v3: rep.Files is the new COMPOSE_ENV_FILES = Layer-1 ONLY, so it equals
	// rep.ChainFiles (both collect only "layer1" entries; D2 keeps both fields).
	// Service env_file: (Layer-2) paths are deliberately absent — they are
	// runtime-only and surface via the --files runtime group from rep.Services.
	// env-debug --chain (and the bare default view) renders ChainFiles so secrets
	// stay last WITHIN the Layer-1 chain (acceptance TestScenario12 [12.4]). The
	// appends happen before the missing-file continue so the listings stay
	// consistent, and before the chain-only early-return so they populate in
	// chain-only mode too.
	for i, f := range in.EnvFiles {
		if f.Layer != "layer1" {
			continue // Layer-2 (service env_file:) is runtime-only — no chain attribution
		}
		rep.Files = append(rep.Files, f.Path)
		rep.ChainFiles = append(rep.ChainFiles, f.Path)
		m := parsed[i]
		if m == nil {
			continue // skip missing (parity)
		}
		if in.WantLayers { // --overview: raw ordered chain layer (separate read; parsed[i] is expanded/unordered)
			if entries, lerr := ParseOrderedLiteral(f.Path); lerr == nil {
				rep.Layers = append(rep.Layers, provenance.OverviewLayer{File: f.Path, Layer: "layer1", Entries: entries})
			}
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
	// compose actually interpolates. We overlay interpEnv (Layer-1 + shell ONLY),
	// NOT chainEnv — A attributes over the interpolation env, so a var defined
	// only in a service env_file: (Layer-2) gets NO chain winner here (its
	// definitions live in RuntimeDefs as gap evidence). When the shell overrides a
	// Layer-1 file, the file becomes Overridden and the winner becomes
	// (environment); a shell-only var (no chain file set it) is attributed to
	// (environment) too.
	envSrc := provenance.Source{File: "(environment)", Layer: "environment"}
	for _, k := range sortedKeys(interpEnv) {
		v := interpEnv[k]
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

	// Set InChain for every var now, BEFORE the chain-only early return below —
	// chain-only mode returns here and never reaches the gap-detection block, so
	// InChain (which renderTrace keys off to pick the normal vs gap view) must be
	// populated for the no-compose-file path too. Gap/RuntimeDefs stay zero-value
	// in chain-only mode (no service env_file: set), which is correct: no compose
	// model means no gap. The gap-detection loop re-sets InChain idempotently for
	// the compose-file path.
	for k, vt := range rep.Vars {
		_, vt.InChain = interpEnv[k]
		rep.Vars[k] = vt
	}

	// chain-only mode: no compose file => return A only.
	if len(configs) == 0 {
		return rep, nil
	}

	// --- C: per-service env with sources (D1 lever keeps environment inline-only) ---
	// Feed interpEnv (Layer-1 + shell ONLY) — NOT chainEnv — to cli.WithEnv so an
	// inline `environment:` value like "${WEB_PORT:-0}" interpolates against exactly
	// what the real v3 run interpolates against (COMPOSE_ENV_FILES = Layer-1, plus
	// host). A service `env_file:` NEVER feeds interpolation (#3435), so an inline
	// ${X} that is defined only in an env_file must resolve to its :-default here —
	// the TRUE container value — never the env_file value (that would make
	// --effective lie, which spec §3 forbids). The env_file LITERAL entries below
	// are read verbatim from svc.EnvFiles and are unaffected by WithEnv.
	mergedEnv := make([]string, 0, len(interpEnv))
	for _, k := range sortedKeys(interpEnv) {
		mergedEnv = append(mergedEnv, k+"="+interpEnv[k])
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
	// runtimeDefs[V] = every service env_file: definition of V (gap evidence;
	// runtime-only — these values apply only inside the service's container, never
	// to interpolation). Built from the SAME dotenv reads the C-loop already does.
	runtimeDefs := map[string][]provenance.ServiceVal{}
	svcNames := make([]string, 0, len(proj.Services))
	for n := range proj.Services {
		svcNames = append(svcNames, n)
	}
	sort.Strings(svcNames)
	for _, name := range svcNames {
		svc := proj.Services[name]
		final := map[string]string{}
		source := map[string]provenance.Source{}
		// Declared env_file: paths in declared order (deduped). Collected
		// independently of parse success / inline overrides so the --files
		// runtime-only group is faithful to what the service LOADS — a file whose
		// every key is later inline-overridden must still appear (N-3 fix). The
		// per-key Entries below may rewrite a source to "(inline environment:)", but
		// that must not erase the file from this declared list.
		var declaredFiles []string
		seenFile := map[string]bool{}
		for _, ef := range svc.EnvFiles {
			if !seenFile[ef.Path] {
				seenFile[ef.Path] = true
				declaredFiles = append(declaredFiles, ef.Path)
				if in.WantLayers { // --overview: one raw ordered layer per distinct declared env_file
					if entries, lerr := ParseOrderedLiteral(ef.Path); lerr == nil {
						rep.Layers = append(rep.Layers, provenance.OverviewLayer{File: ef.Path, Layer: "env_file", Service: name, Entries: entries})
					}
				}
			}
			m, perr := parseDotEnv(ef.Path, lookup)
			if perr != nil {
				continue
			}
			for k, v := range m {
				final[k] = v
				source[k] = provenance.Source{File: ef.Path, Layer: "env_file"}
				runtimeDefs[k] = append(runtimeDefs[k], provenance.ServiceVal{Service: name, File: ef.Path, Value: v})
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
		// --overview: inline `environment:` pseudo-layer LAST per service (inline
		// overrides env_file: — the true container precedence). Raw *string values
		// (NOT interpolated — the C-load uses WithoutEnvironmentResolution), key-sorted
		// for determinism. Emitted only when the service declares any environment:.
		if in.WantLayers && len(svc.Environment) > 0 {
			inlineKeys := make([]string, 0, len(svc.Environment))
			for k := range svc.Environment {
				inlineKeys = append(inlineKeys, k)
			}
			sort.Strings(inlineKeys)
			inline := provenance.OverviewLayer{File: "(inline environment:)", Layer: "environment", Service: name}
			for _, k := range inlineKeys {
				val := ""
				if vp := svc.Environment[k]; vp != nil {
					val = *vp
				}
				inline.Entries = append(inline.Entries, provenance.OverviewEntry{Key: k, RawValue: val})
			}
			rep.Layers = append(rep.Layers, inline)
		}
		se := provenance.ServiceEnv{Service: name, EnvFiles: declaredFiles}
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
	details.Environment = types.Mapping(interpEnv) // cosmetic (SkipInterpolation) but kept honest: L1+shell only
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
	// --- gap detection (pure Go set logic over the two env contexts) ---
	// For each var: InChain = resolvable from the interpolation (Layer-1 + shell)
	// env; RuntimeDefs = its service env_file: definitions; Gap when the var is
	// REFERENCED in a service field (has Effects), is NOT in the interpolation
	// chain, yet IS defined in some service env_file: — precisely the #3435
	// footprint (the value sits in an env_file but the run interpolates the
	// :-default). Effects each carry the same Gap so renderers/JSON see it per site.
	for k, vt := range rep.Vars {
		sort.Slice(vt.Effects, func(i, j int) bool { // deterministic effects order
			if vt.Effects[i].Service != vt.Effects[j].Service {
				return vt.Effects[i].Service < vt.Effects[j].Service
			}
			return vt.Effects[i].Field < vt.Effects[j].Field
		})
		_, vt.InChain = interpEnv[k]
		vt.RuntimeDefs = runtimeDefs[k]
		sort.Slice(vt.RuntimeDefs, func(i, j int) bool { // stable JSON
			if vt.RuntimeDefs[i].Service != vt.RuntimeDefs[j].Service {
				return vt.RuntimeDefs[i].Service < vt.RuntimeDefs[j].Service
			}
			return vt.RuntimeDefs[i].File < vt.RuntimeDefs[j].File
		})
		referenced := len(vt.Effects) > 0
		vt.Gap = referenced && !vt.InChain && len(vt.RuntimeDefs) > 0
		for i := range vt.Effects {
			vt.Effects[i].Gap = vt.Gap
		}
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
