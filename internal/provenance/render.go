package provenance

import (
	"encoding/json"
	"fmt"
	"io"
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
