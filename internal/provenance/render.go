package provenance

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

// Styler renders semantic elements with optional color/formatting. It is defined
// HERE (not in internal/style) so internal/provenance imports NO styling library —
// the lipgloss-backed implementation lives in internal/style and is injected via
// HumanOpts.Style. A nil Style falls back to plainStyler (byte-identical plain),
// so callers that don't set it (and the existing render tests) are unchanged.
//
// Methods either style a passed string (Header(s) → styled s) or, for the markers
// and arrow which have no payload, return the styled glyph (MarkerNew() → "+").
type Styler interface {
	Header(s string) string  // section headers — bold cyan
	MarkerNew() string       // "+" new — green
	MarkerOverride() string  // "~" override — yellow
	MarkerRepeat() string    // "·" repeat — dim
	Arrow() string           // "→" between old/new — dim
	Key(s string) string     // KEY name — bold
	Value(s string) string   // a value — normal/readable
	Old(s string) string     // the shadowed (old) value — dim
	Path(s string) string    // file path — cyan
	Service(s string) string // service name — bold magenta
	SourceLabel(s string) string
	Gap(s string) string     // gap line body — red
	GapName(s string) string // the gapped var name — bold red
	Hint(s string) string    // a dim advisory line (e.g. empty-chain hint) — dim
	Ok(s string) string      // validate ok — green
	Fail(s string) string    // validate fail — red
	Created(s string) string // init created — green
	Skipped(s string) string // init skipped — dim
	ErrorMsg(s string) string
}

// plainStyler is the nil-safe fallback: every method returns its arg unchanged,
// and the markers/arrow return their plain glyphs. This keeps human output
// byte-identical to the pre-color behavior whenever no Styler is injected.
type plainStyler struct{}

func (plainStyler) Header(s string) string      { return s }
func (plainStyler) MarkerNew() string           { return "+" }
func (plainStyler) MarkerOverride() string      { return "~" }
func (plainStyler) MarkerRepeat() string        { return "·" }
func (plainStyler) Arrow() string               { return "→" }
func (plainStyler) Key(s string) string         { return s }
func (plainStyler) Value(s string) string       { return s }
func (plainStyler) Old(s string) string         { return s }
func (plainStyler) Path(s string) string        { return s }
func (plainStyler) Service(s string) string     { return s }
func (plainStyler) SourceLabel(s string) string { return s }
func (plainStyler) Gap(s string) string         { return s }
func (plainStyler) GapName(s string) string     { return s }
func (plainStyler) Hint(s string) string        { return s }
func (plainStyler) Ok(s string) string          { return s }
func (plainStyler) Fail(s string) string        { return s }
func (plainStyler) Created(s string) string     { return s }
func (plainStyler) Skipped(s string) string     { return s }
func (plainStyler) ErrorMsg(s string) string    { return s }

// st returns the injected Styler or a plainStyler when none was set (nil-safe).
func st(s Styler) Styler {
	if s == nil {
		return plainStyler{}
	}
	return s
}

// HumanOpts selects which view RenderHuman emits. Exactly one mode is active at a
// time; the switch in RenderHuman picks the first set field in precedence order.
type HumanOpts struct {
	Trace     string // non-empty: single-var trace (A + B-lite)
	Effective bool   // per-service env (C)
	Service   string // filter Effective to one service
	Chain     bool   // Layer-1-only list (default view) — renders Report.ChainFiles
	Files     bool   // two-group view: interpolation (Report.Files) + runtime-only (Report.Services)
	Value     string // non-empty: print the winning value only
	Overview  bool   // per-file layering overview (raw values, +/~/· markers) — renders Report.Layers

	// Header inputs for the --overview mode (best-effort; from chain.Resolve + cmd).
	ComposeEnv       string // resolved COMPOSE_ENV value
	ComposeEnvSource string // where it came from: "shell" | ".env" | "default"
	ProjectDir       string // project dir (basename used as the overview title)

	Style Styler // optional color/formatting; nil ⇒ plain (byte-identical)
}

// RenderJSON writes the whole Report as indented JSON.
func RenderJSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// RenderHuman writes the selected view as plain (or styled) text. The styler is
// resolved once (nil ⇒ plain) and threaded into each view.
func RenderHuman(w io.Writer, r Report, o HumanOpts) {
	s := st(o.Style)
	switch {
	case o.Value != "":
		fmt.Fprintln(w, r.Vars[o.Value].Value) // a bare value, never styled (script-friendly)
	case o.Trace != "":
		renderTrace(w, r, o.Trace, s)
	case o.Effective:
		renderEffective(w, r, o.Service, s)
	case o.Files:
		renderFiles(w, r, s)
	case o.Overview:
		renderOverview(w, r, o, s)
	default: // Chain == default view: Layer-1 only (Report.ChainFiles)
		if len(r.ChainFiles) == 0 {
			fmt.Fprintln(w, s.Hint(emptyChainHint))
			return
		}
		for _, f := range r.ChainFiles {
			fmt.Fprintln(w, s.Path(f))
		}
	}
}

// emptyChainHint is the dim advisory shown in the human env-debug views when the
// Layer-1 interpolation chain has ZERO existing files — otherwise those views print
// an empty section that reads as broken (FIX 1). Kept neutral (does not assume
// example.* exist). NOT emitted by the `env-files` command (machine output: an
// empty stdout is correct for piping).
const emptyChainHint = "(none — no Layer-1 chain files present; run `cenvkit init` or add .env)"

// renderTrace prints one variable's story. Post-v3 it is gap-aware: a var present
// in the interpolation (Layer-1) chain renders the normal winner/overridden/effects
// view; a var defined only in a service env_file: (a gap) renders the
// interpolation/runtime split + a ⚠ gap line per effect + a fix hint, explaining
// that the env_file value is container-only and never reaches the run's ${VAR}.
func renderTrace(w io.Writer, r Report, name string, s Styler) {
	vt, ok := r.Vars[name]
	if !ok {
		fmt.Fprintf(w, "%s: not set\n", name)
		return
	}
	if vt.InChain {
		// Normal path: the var resolves from the Layer-1 chain (or shell) — no gap.
		fmt.Fprintf(w, "%s=%s\n", s.Key(name), s.Value(vt.Value))
		fmt.Fprintf(w, "  winner:     %s (%s)\n", s.Path(vt.Winner.File), s.SourceLabel(vt.Winner.Layer))
		for _, src := range vt.Overridden {
			fmt.Fprintf(w, "  overridden: %s (%s)\n", s.Path(src.File), s.SourceLabel(src.Layer))
		}
		for _, e := range vt.Effects {
			fmt.Fprintf(w, "  effect:     service %s field %s -> %s\n", s.Service(e.Service), e.Field, s.Value(stripVarPrefix(name, e.Resolved)))
		}
		return
	}
	// Not in the interpolation chain: show the interpolation/runtime split.
	fmt.Fprintln(w, s.Key(name))
	fmt.Fprintln(w, "  interpolation: NOT in the Layer-1 chain -> ${"+name+"} falls back at run time")
	for _, rd := range vt.RuntimeDefs {
		fmt.Fprintf(w, "  runtime:       %s -> %s=%s  (service `%s` container env only)\n",
			s.Path(rd.File), name, s.Value(rd.Value), s.Service(rd.Service))
	}
	if vt.Gap {
		for _, e := range vt.Effects {
			fmt.Fprintf(w, "  %s\n", s.Gap(fmt.Sprintf("⚠ gap: ${%s} used in service %s %s resolves to %q at the run, NOT the env_file value (defined only in a service env_file).",
				name, e.Service, e.Field, stripVarPrefix(name, e.Resolved))))
		}
		fmt.Fprintf(w, "  fix:   add %s to the Layer-1 chain (e.g. .env), or use it runtime-only.\n", name)
	}
}

// renderFiles prints the two labeled groups (D2 / spec §3.4): the interpolation
// set (COMPOSE_ENV_FILES = Layer 1) and the runtime-only set (service env_file:
// paths grouped by service, NOT interpolated — container env only).
func renderFiles(w io.Writer, r Report, s Styler) {
	fmt.Fprintln(w, s.Header("interpolation (COMPOSE_ENV_FILES):"))
	if len(r.Files) == 0 {
		fmt.Fprintf(w, "  %s\n", s.Hint(emptyChainHint)) // FIX 1
	}
	for _, f := range r.Files {
		fmt.Fprintf(w, "  %s\n", s.Path(f))
	}
	fmt.Fprintln(w, s.Header("runtime-only (service env_file: — NOT interpolated, container env only):"))
	for _, se := range r.Services {
		// Render the service's DECLARED env_file: paths (ServiceEnv.EnvFiles), not
		// the per-key Entries sources — a file whose every key is inline-overridden
		// still appears here (N-3 fix). The engine supplies these deduped in
		// declared order.
		if len(se.EnvFiles) == 0 {
			continue
		}
		fmt.Fprintf(w, "  %s:\n", s.Service(se.Service))
		for _, f := range se.EnvFiles {
			fmt.Fprintf(w, "    %s\n", s.Path(f))
		}
	}
}

// renderOverview prints the two-section layering overview (spec §2/§3): a header,
// then the interpolation chain (Layer-1) with a single accumulator, then the
// runtime-only service env_file: layers with a FRESH accumulator per service and
// an `inline environment:` pseudo-layer last, plus a `⚠ gap:` line per service for
// any var that is referenced as ${VAR} but defined only in that service's
// env_file:. Markers are derived here from the accumulator walk (not stored):
//
//   - new        the key is first defined in this layer
//     ~ override   an earlier layer set it → show `old → new`
//     · repeat     set again to the same value
func renderOverview(w io.Writer, r Report, o HumanOpts, s Styler) {
	// Header (best-effort, like the sh kit).
	title := filepath.Base(o.ProjectDir)
	if title == "" || title == "." || title == string(filepath.Separator) {
		title = o.ProjectDir
	}
	fmt.Fprintln(w, s.Header(fmt.Sprintf("env overview — %s (mode: overview)", title)))
	if o.ComposeEnv != "" {
		if o.ComposeEnvSource != "" {
			fmt.Fprintf(w, "  COMPOSE_ENV = %s (from %s)\n", s.Value(o.ComposeEnv), s.SourceLabel(o.ComposeEnvSource))
		} else {
			fmt.Fprintf(w, "  COMPOSE_ENV = %s\n", s.Value(o.ComposeEnv))
		}
	}
	if o.ProjectDir != "" {
		fmt.Fprintf(w, "  Project dir = %s\n", s.Path(o.ProjectDir))
	}

	// Section 1 — interpolation chain (Layer-1 only).
	fmt.Fprintln(w, "\n"+s.Header("Interpolation chain (COMPOSE_ENV_FILES)"))
	fmt.Fprintf(w, "  %s new   %s override   %s repeat\n", s.MarkerNew(), s.MarkerOverride(), s.MarkerRepeat())
	chainAcc := map[string]string{}
	chainLayers := 0
	for _, l := range r.Layers {
		if l.Layer != "layer1" {
			continue
		}
		chainLayers++
		fmt.Fprintf(w, "\n  %s\n", s.Path(l.File))
		renderLayerEntries(w, l.Entries, chainAcc, s)
	}
	if chainLayers == 0 {
		fmt.Fprintf(w, "\n  %s\n", s.Hint(emptyChainHint)) // FIX 1
	}

	// Section 2 — runtime-only, per service (fresh accumulator each). Suppressed
	// entirely when no service has env_file:/inline layers (e.g. chain-only mode),
	// so the section header isn't printed with nothing under it (N-4).
	services := overviewServices(r.Layers)
	if len(services) == 0 {
		return
	}
	fmt.Fprintln(w, "\n"+s.Header("Runtime-only — service env_file: (NOT interpolated)"))
	for _, name := range services {
		fmt.Fprintf(w, "  %s:\n", s.Service(name))
		svcAcc := map[string]string{}
		for _, l := range r.Layers {
			if l.Service != name {
				continue
			}
			label := l.File
			if l.Layer == "environment" {
				label = "inline environment:"
			}
			fmt.Fprintf(w, "    %s\n", s.Path(label))
			renderLayerEntriesIndented(w, l.Entries, svcAcc, s)
		}
		renderServiceGaps(w, r, name, s)
	}
}

// renderLayerEntries prints one chain layer's entries with +/~/· markers against
// acc (6-space indent for chain entries), updating acc as it goes.
func renderLayerEntries(w io.Writer, entries []OverviewEntry, acc map[string]string, s Styler) {
	renderEntriesAt(w, entries, acc, s, "      ")
}

// renderLayerEntriesIndented is renderLayerEntries with the deeper runtime-section
// indent (per the spec example: service > layer > entry).
func renderLayerEntriesIndented(w io.Writer, entries []OverviewEntry, acc map[string]string, s Styler) {
	renderEntriesAt(w, entries, acc, s, "        ")
}

// renderEntriesAt walks entries applying +/~/· markers (styled) and the key/value/
// old/arrow styling at the given indent. Marker semantics are identical to before;
// only the rendered glyph/value strings pass through the styler (plain ⇒ identical).
func renderEntriesAt(w io.Writer, entries []OverviewEntry, acc map[string]string, s Styler, indent string) {
	for _, e := range entries {
		kind, old, seen := classify(acc, e)
		key := s.Key(e.Key)
		switch {
		case !seen:
			fmt.Fprintf(w, "%s%s %s = %s\n", indent, s.MarkerNew(), key, s.Value(e.RawValue))
		case kind == "~":
			fmt.Fprintf(w, "%s%s %s = %s %s %s\n", indent, s.MarkerOverride(), key, s.Old(old), s.Arrow(), s.Value(e.RawValue))
		default: // ·
			fmt.Fprintf(w, "%s%s %s = %s\n", indent, s.MarkerRepeat(), key, s.Value(e.RawValue))
		}
		acc[e.Key] = e.RawValue
	}
}

// classify returns the marker KIND for an entry against the accumulator: "+" when
// the key is unseen, "~" when seen with a different value (old = the prior value),
// "·" when seen with the same value. (The styled glyph is chosen by the caller.)
func classify(acc map[string]string, e OverviewEntry) (kind, old string, seen bool) {
	prev, ok := acc[e.Key]
	if !ok {
		return "+", "", false
	}
	if prev != e.RawValue {
		return "~", prev, true
	}
	return "·", prev, true
}

// overviewServices returns the distinct service names present in the runtime
// layers, in first-seen order (the engine emits services in sorted order).
func overviewServices(layers []OverviewLayer) []string {
	seen := map[string]bool{}
	var names []string
	for _, l := range layers {
		if l.Service == "" || seen[l.Service] {
			continue
		}
		seen[l.Service] = true
		names = append(names, l.Service)
	}
	return names
}

// renderServiceGaps prints a `⚠ gap:` line per var that is a v3 gap (referenced as
// ${VAR} but env_file-only) AND referenced in THIS service — reusing the existing
// gap detection (Vars[].Gap / Effects), pure presentation.
func renderServiceGaps(w io.Writer, r Report, service string, s Styler) {
	names := make([]string, 0, len(r.Vars))
	for name := range r.Vars {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		vt := r.Vars[name]
		if !vt.Gap {
			continue
		}
		var fields []string
		fallback := ""
		for _, e := range vt.Effects {
			if e.Service == service {
				fields = append(fields, e.Field)
				if fallback == "" {
					fallback = stripVarPrefix(name, e.Resolved) // run's fallback, normalized (FIX 2)
				}
			}
		}
		if len(fields) == 0 {
			continue // gap is in another service
		}
		// The whole gap line is red; the gapped var name is bold red within it.
		fmt.Fprintf(w, "    %s\n", s.Gap(fmt.Sprintf("⚠ gap: %s — used as ${%s} in service %s (%s)",
			s.GapName(name), name, service, strings.Join(fields, ", "))))
		fmt.Fprintf(w, "    %s\n", s.Gap(fmt.Sprintf("  but NOT in the Layer-1 chain → run falls back to %q.", fallback)))
	}
}

// stripVarPrefix normalizes a resolved value for HUMAN display: a list-form
// `environment: ["NAME=${NAME:-0}"]` entry resolves to the whole "NAME=0" (the leaf
// includes the key), while map-form `environment: {NAME: ...}` resolves to bare
// "0". Strip a leading "NAME=" so both render the same value (FIX 2). Only the
// exact "<name>=" prefix is stripped — a non-environment leaf like ports "0:80"
// (no "<name>=") is left untouched. The stored Report.Effect.Resolved keeps the raw
// value (for --json); this strip is render-time only.
func stripVarPrefix(name, resolved string) string {
	if p := name + "="; strings.HasPrefix(resolved, p) {
		return resolved[len(p):]
	}
	return resolved
}

// renderEffective prints each service's effective env. Entries are pre-sorted by
// the engine (deterministic emit), so we iterate as-is.
func renderEffective(w io.Writer, r Report, service string, s Styler) {
	for _, se := range r.Services {
		if service != "" && se.Service != service {
			continue
		}
		fmt.Fprintf(w, "service %s:\n", s.Service(se.Service))
		for _, e := range se.Entries {
			fmt.Fprintf(w, "  %s=%s\t<- %s (%s)\n",
				s.Key(e.Key), s.Value(e.Value), s.Path(e.Source.File), s.SourceLabel(e.Source.Layer))
		}
	}
}
