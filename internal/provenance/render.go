package provenance

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

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
}

// RenderJSON writes the whole Report as indented JSON.
func RenderJSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// RenderHuman writes the selected view as plain text.
func RenderHuman(w io.Writer, r Report, o HumanOpts) {
	switch {
	case o.Value != "":
		fmt.Fprintln(w, r.Vars[o.Value].Value)
	case o.Trace != "":
		renderTrace(w, r, o.Trace)
	case o.Effective:
		renderEffective(w, r, o.Service)
	case o.Files:
		renderFiles(w, r)
	case o.Overview:
		renderOverview(w, r, o)
	default: // Chain == default view: Layer-1 only (Report.ChainFiles)
		for _, f := range r.ChainFiles {
			fmt.Fprintln(w, f)
		}
	}
}

// renderTrace prints one variable's story. Post-v3 it is gap-aware: a var present
// in the interpolation (Layer-1) chain renders the normal winner/overridden/effects
// view; a var defined only in a service env_file: (a gap) renders the
// interpolation/runtime split + a ⚠ gap line per effect + a fix hint, explaining
// that the env_file value is container-only and never reaches the run's ${VAR}.
func renderTrace(w io.Writer, r Report, name string) {
	vt, ok := r.Vars[name]
	if !ok {
		fmt.Fprintf(w, "%s: not set\n", name)
		return
	}
	if vt.InChain {
		// Normal path: the var resolves from the Layer-1 chain (or shell) — no gap.
		fmt.Fprintf(w, "%s=%s\n", name, vt.Value)
		fmt.Fprintf(w, "  winner:     %s (%s)\n", vt.Winner.File, vt.Winner.Layer)
		for _, s := range vt.Overridden {
			fmt.Fprintf(w, "  overridden: %s (%s)\n", s.File, s.Layer)
		}
		for _, e := range vt.Effects {
			fmt.Fprintf(w, "  effect:     service %s field %s -> %s\n", e.Service, e.Field, e.Resolved)
		}
		return
	}
	// Not in the interpolation chain: show the interpolation/runtime split.
	fmt.Fprintln(w, name)
	fmt.Fprintln(w, "  interpolation: NOT in the Layer-1 chain -> ${"+name+"} falls back at run time")
	for _, rd := range vt.RuntimeDefs {
		fmt.Fprintf(w, "  runtime:       %s -> %s=%s  (service `%s` container env only)\n", rd.File, name, rd.Value, rd.Service)
	}
	if vt.Gap {
		for _, e := range vt.Effects {
			fmt.Fprintf(w, "  ⚠ gap: ${%s} used in service %s %s resolves to %q at the run, NOT the env_file value (defined only in a service env_file).\n",
				name, e.Service, e.Field, e.Resolved)
		}
		fmt.Fprintf(w, "  fix:   add %s to the Layer-1 chain (e.g. .env), or use it runtime-only.\n", name)
	}
}

// renderFiles prints the two labeled groups (D2 / spec §3.4): the interpolation
// set (COMPOSE_ENV_FILES = Layer 1) and the runtime-only set (service env_file:
// paths grouped by service, NOT interpolated — container env only).
func renderFiles(w io.Writer, r Report) {
	fmt.Fprintln(w, "interpolation (COMPOSE_ENV_FILES):")
	for _, f := range r.Files {
		fmt.Fprintf(w, "  %s\n", f)
	}
	fmt.Fprintln(w, "runtime-only (service env_file: — NOT interpolated, container env only):")
	for _, se := range r.Services {
		// Render the service's DECLARED env_file: paths (ServiceEnv.EnvFiles), not
		// the per-key Entries sources — a file whose every key is inline-overridden
		// still appears here (N-3 fix). The engine supplies these deduped in
		// declared order.
		if len(se.EnvFiles) == 0 {
			continue
		}
		fmt.Fprintf(w, "  %s:\n", se.Service)
		for _, f := range se.EnvFiles {
			fmt.Fprintf(w, "    %s\n", f)
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
func renderOverview(w io.Writer, r Report, o HumanOpts) {
	// Header (best-effort, like the sh kit).
	title := filepath.Base(o.ProjectDir)
	if title == "" || title == "." || title == string(filepath.Separator) {
		title = o.ProjectDir
	}
	fmt.Fprintf(w, "env overview — %s (mode: overview)\n", title)
	if o.ComposeEnv != "" {
		if o.ComposeEnvSource != "" {
			fmt.Fprintf(w, "  COMPOSE_ENV = %s (from %s)\n", o.ComposeEnv, o.ComposeEnvSource)
		} else {
			fmt.Fprintf(w, "  COMPOSE_ENV = %s\n", o.ComposeEnv)
		}
	}
	if o.ProjectDir != "" {
		fmt.Fprintf(w, "  Project dir = %s\n", o.ProjectDir)
	}

	// Section 1 — interpolation chain (Layer-1 only).
	fmt.Fprintln(w, "\nInterpolation chain (COMPOSE_ENV_FILES)")
	fmt.Fprintln(w, "  + new   ~ override   · repeat")
	chainAcc := map[string]string{}
	for _, l := range r.Layers {
		if l.Layer != "layer1" {
			continue
		}
		fmt.Fprintf(w, "\n  %s\n", l.File)
		renderLayerEntries(w, l.Entries, chainAcc)
	}

	// Section 2 — runtime-only, per service (fresh accumulator each). Suppressed
	// entirely when no service has env_file:/inline layers (e.g. chain-only mode),
	// so the section header isn't printed with nothing under it (N-4).
	services := overviewServices(r.Layers)
	if len(services) == 0 {
		return
	}
	fmt.Fprintln(w, "\nRuntime-only — service env_file: (NOT interpolated)")
	for _, name := range services {
		fmt.Fprintf(w, "  %s:\n", name)
		svcAcc := map[string]string{}
		for _, l := range r.Layers {
			if l.Service != name {
				continue
			}
			label := l.File
			if l.Layer == "environment" {
				label = "inline environment:"
			}
			fmt.Fprintf(w, "    %s\n", label)
			renderLayerEntriesIndented(w, l.Entries, svcAcc)
		}
		renderServiceGaps(w, r, name)
	}
}

// renderLayerEntries prints one chain layer's entries with +/~/· markers against
// acc (8-space indent for chain entries), updating acc as it goes.
func renderLayerEntries(w io.Writer, entries []OverviewEntry, acc map[string]string) {
	for _, e := range entries {
		marker, old, seen := classify(acc, e)
		switch {
		case !seen:
			fmt.Fprintf(w, "      %s %s = %s\n", marker, e.Key, e.RawValue)
		case marker == "~":
			fmt.Fprintf(w, "      %s %s = %s → %s\n", marker, e.Key, old, e.RawValue)
		default: // ·
			fmt.Fprintf(w, "      %s %s = %s\n", marker, e.Key, e.RawValue)
		}
		acc[e.Key] = e.RawValue
	}
}

// renderLayerEntriesIndented is renderLayerEntries with the deeper runtime-section
// indent (per the spec example: service > layer > entry).
func renderLayerEntriesIndented(w io.Writer, entries []OverviewEntry, acc map[string]string) {
	for _, e := range entries {
		marker, old, seen := classify(acc, e)
		switch {
		case !seen:
			fmt.Fprintf(w, "        %s %s = %s\n", marker, e.Key, e.RawValue)
		case marker == "~":
			fmt.Fprintf(w, "        %s %s = %s → %s\n", marker, e.Key, old, e.RawValue)
		default: // ·
			fmt.Fprintf(w, "        %s %s = %s\n", marker, e.Key, e.RawValue)
		}
		acc[e.Key] = e.RawValue
	}
}

// classify returns the +/~/· marker for an entry against the accumulator: "+" when
// the key is unseen, "~" when seen with a different value (old = the prior value),
// "·" when seen with the same value.
func classify(acc map[string]string, e OverviewEntry) (marker, old string, seen bool) {
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
func renderServiceGaps(w io.Writer, r Report, service string) {
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
					fallback = e.Resolved // the run's actual fallback value (N-2)
				}
			}
		}
		if len(fields) == 0 {
			continue // gap is in another service
		}
		fmt.Fprintf(w, "    ⚠ gap: %s — used as ${%s} in service %s (%s)\n",
			name, name, service, strings.Join(fields, ", "))
		fmt.Fprintf(w, "      but NOT in the Layer-1 chain → run falls back to %q.\n", fallback)
	}
}

// renderEffective prints each service's effective env. Entries are pre-sorted by
// the engine (deterministic emit), so we iterate as-is.
func renderEffective(w io.Writer, r Report, service string) {
	for _, se := range r.Services {
		if service != "" && se.Service != service {
			continue
		}
		fmt.Fprintf(w, "service %s:\n", se.Service)
		for _, e := range se.Entries {
			fmt.Fprintf(w, "  %s=%s\t<- %s (%s)\n", e.Key, e.Value, e.Source.File, e.Source.Layer)
		}
	}
}
