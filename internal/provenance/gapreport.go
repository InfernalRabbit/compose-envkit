package provenance

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// GapSite is one place an env_file:-only ${VAR} reaches the compose model and so
// falls back at the real run (the #3435 gap). Field names mirror Effect
// (service/field); Fallback is the value the run interpolates (Effect.Resolved,
// human-normalized via stripVarPrefix).
type GapSite struct {
	Var      string `json:"var"`
	Service  string `json:"service"`
	Field    string `json:"field"`
	Fallback string `json:"fallback"`
	Fix      string `json:"fix"`
}

// GapReport is the gap-report projection of a Report: every gap site + a count.
type GapReport struct {
	Gaps  []GapSite `json:"gaps"`
	Count int       `json:"count"`
}

// CollectGaps projects a Report into its gap sites. Order is deterministic: var
// name, then the engine-sorted Effect order (service, field). A gap site is every
// Effect of a var whose Gap is true (engine already set Gap = referenced &&
// !InChain && len(RuntimeDefs)>0, internal/engine/provenance.go:424).
func CollectGaps(r Report) GapReport {
	names := make([]string, 0, len(r.Vars))
	for name := range r.Vars {
		names = append(names, name)
	}
	sort.Strings(names)
	gr := GapReport{}
	for _, name := range names {
		vt := r.Vars[name]
		if !vt.Gap {
			continue
		}
		fix := fmt.Sprintf("add %s to the Layer-1 chain (e.g. .env), or use it runtime-only", name)
		for _, e := range vt.Effects {
			gr.Gaps = append(gr.Gaps, GapSite{
				Var:      name,
				Service:  e.Service,
				Field:    e.Field,
				Fallback: stripVarPrefix(name, e.Resolved),
				Fix:      fix,
			})
		}
	}
	gr.Count = len(gr.Gaps)
	return gr
}

// RenderGapReportJSON writes the gap report as indented JSON (never styled).
func RenderGapReportJSON(w io.Writer, gr GapReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(gr)
}

// RenderGapReportHuman writes a human gap report: one ⚠ line per gap site plus a
// red summary, or a single green ok line when clean. nil styler => plain.
func RenderGapReportHuman(w io.Writer, gr GapReport, s Styler) {
	sty := st(s)
	if gr.Count == 0 {
		_, _ = fmt.Fprintln(w, sty.Ok("no env_file→interpolation gaps"))
		return
	}
	for _, g := range gr.Gaps {
		_, _ = fmt.Fprintf(w, "%s\n", sty.Gap(fmt.Sprintf(
			"⚠ gap: ${%s} used in service %s %s resolves to %q at the run (defined only in a service env_file:).",
			g.Var, g.Service, g.Field, g.Fallback)))
	}
	_, _ = fmt.Fprintf(w, "%s\n", sty.Fail(fmt.Sprintf("%d gap(s) found", gr.Count)))
}
