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
	Files     bool   // full Report.Files list (Layer-1 + Layer-2)
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
		for _, f := range r.Files {
			fmt.Fprintln(w, f)
		}
	default: // Chain == default view: Layer-1 only (Report.ChainFiles)
		for _, f := range r.ChainFiles {
			fmt.Fprintln(w, f)
		}
	}
}

func renderTrace(w io.Writer, r Report, name string) {
	vt, ok := r.Vars[name]
	if !ok {
		fmt.Fprintf(w, "%s: not set\n", name)
		return
	}
	fmt.Fprintf(w, "%s=%s\n", name, vt.Value)
	fmt.Fprintf(w, "  winner:     %s (%s)\n", vt.Winner.File, vt.Winner.Layer)
	for _, s := range vt.Overridden {
		fmt.Fprintf(w, "  overridden: %s (%s)\n", s.File, s.Layer)
	}
	for _, e := range vt.Effects {
		fmt.Fprintf(w, "  effect:     service %s field %s -> %s\n", e.Service, e.Field, e.Resolved)
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
