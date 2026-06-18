package provenance

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestCollectGaps(t *testing.T) {
	r := Report{Vars: map[string]VarTrace{
		// a real gap: referenced, not in chain, env_file-only — engine set Gap=true.
		"WEB_PORT": {
			Name: "WEB_PORT", Gap: true,
			Effects: []Effect{{Service: "web", Field: "ports[0]", Resolved: "0:80", Gap: true}},
		},
		// list-form leaf carries the "KEY=" prefix; Fallback must be normalized.
		"DB_HOST": {
			Name: "DB_HOST", Gap: true,
			Effects: []Effect{{Service: "api", Field: "environment[0]", Resolved: "DB_HOST=", Gap: true}},
		},
		// not a gap: in the chain — must be excluded.
		"IN_CHAIN": {Name: "IN_CHAIN", InChain: true,
			Effects: []Effect{{Service: "web", Field: "image", Resolved: "nginx"}}},
	}}
	got := CollectGaps(r)
	want := GapReport{Count: 2, Gaps: []GapSite{
		{Var: "DB_HOST", Service: "api", Field: "environment[0]", Fallback: "",
			Fix: "add DB_HOST to the Layer-1 chain (e.g. .env), or use it runtime-only"},
		{Var: "WEB_PORT", Service: "web", Field: "ports[0]", Fallback: "0:80",
			Fix: "add WEB_PORT to the Layer-1 chain (e.g. .env), or use it runtime-only"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CollectGaps mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

// TestCollectGaps_NoGaps: a report with only InChain vars produces an empty GapReport.
func TestCollectGaps_NoGaps(t *testing.T) {
	r := Report{Vars: map[string]VarTrace{
		"CLEAN": {Name: "CLEAN", InChain: true,
			Effects: []Effect{{Service: "web", Field: "image", Resolved: "busybox"}}},
	}}
	got := CollectGaps(r)
	if got.Count != 0 || len(got.Gaps) != 0 {
		t.Fatalf("expected empty GapReport, got %#v", got)
	}
}

// TestCollectGaps_Empty: empty report produces empty GapReport.
func TestCollectGaps_Empty(t *testing.T) {
	got := CollectGaps(Report{})
	if got.Count != 0 {
		t.Fatalf("expected Count=0, got %d", got.Count)
	}
}

// TestCollectGaps_SortedByVar: vars are sorted alphabetically so output is deterministic.
func TestCollectGaps_SortedByVar(t *testing.T) {
	r := Report{Vars: map[string]VarTrace{
		"ZZZ_PORT": {
			Name: "ZZZ_PORT", Gap: true,
			Effects: []Effect{{Service: "web", Field: "ports[0]", Resolved: "0:80", Gap: true}},
		},
		"AAA_PORT": {
			Name: "AAA_PORT", Gap: true,
			Effects: []Effect{{Service: "api", Field: "ports[1]", Resolved: "0:81", Gap: true}},
		},
	}}
	got := CollectGaps(r)
	if got.Count != 2 {
		t.Fatalf("expected Count=2, got %d", got.Count)
	}
	if got.Gaps[0].Var != "AAA_PORT" || got.Gaps[1].Var != "ZZZ_PORT" {
		t.Fatalf("expected alphabetical order [AAA_PORT, ZZZ_PORT], got [%s, %s]",
			got.Gaps[0].Var, got.Gaps[1].Var)
	}
}

// TestRenderGapReportJSON: required fields present; output must never contain ANSI.
func TestRenderGapReportJSON(t *testing.T) {
	gr := GapReport{Count: 1, Gaps: []GapSite{
		{Var: "WEB_PORT", Service: "web", Field: "ports[0]", Fallback: "0:80",
			Fix: "add WEB_PORT to the Layer-1 chain (e.g. .env), or use it runtime-only"},
	}}
	var b bytes.Buffer
	if err := RenderGapReportJSON(&b, gr); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{`"count": 1`, `"var": "WEB_PORT"`, `"service": "web"`,
		`"field": "ports[0]"`, `"fallback": "0:80"`, `"fix":`} {
		if !strings.Contains(got, want) {
			t.Fatalf("json missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("json must never be styled:\n%s", got)
	}
}

// TestRenderGapReportJSON_Empty: zero-gap report encodes count=0 and empty gaps array.
func TestRenderGapReportJSON_Empty(t *testing.T) {
	var b bytes.Buffer
	if err := RenderGapReportJSON(&b, GapReport{}); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if !strings.Contains(got, `"count": 0`) {
		t.Fatalf("empty gap report JSON missing count:0:\n%s", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("json must never be styled:\n%s", got)
	}
}

// TestRenderGapReportHumanClean: no gaps → the exact clean line.
func TestRenderGapReportHumanClean(t *testing.T) {
	var b bytes.Buffer
	RenderGapReportHuman(&b, GapReport{}, nil) // nil styler => plain
	if got := b.String(); got != "no env_file→interpolation gaps\n" {
		t.Fatalf("clean output = %q", got)
	}
}

// TestRenderGapReportHumanGaps: one gap → ⚠ line + N gap(s) found summary.
func TestRenderGapReportHumanGaps(t *testing.T) {
	gr := GapReport{Count: 1, Gaps: []GapSite{
		{Var: "WEB_PORT", Service: "web", Field: "ports[0]", Fallback: "0:80"},
	}}
	var b bytes.Buffer
	RenderGapReportHuman(&b, gr, nil)
	got := b.String()
	if !strings.Contains(got, "⚠ gap: ${WEB_PORT} used in service web ports[0] resolves to \"0:80\"") {
		t.Fatalf("gap line missing:\n%s", got)
	}
	if !strings.Contains(got, "1 gap(s) found") {
		t.Fatalf("summary missing:\n%s", got)
	}
}

// TestRenderGapReportHumanGaps_MultipleGaps: two gaps → two ⚠ lines + "2 gap(s) found".
func TestRenderGapReportHumanGaps_MultipleGaps(t *testing.T) {
	gr := GapReport{Count: 2, Gaps: []GapSite{
		{Var: "AAA", Service: "svc1", Field: "ports[0]", Fallback: "0:80"},
		{Var: "BBB", Service: "svc2", Field: "environment[0]", Fallback: ""},
	}}
	var b bytes.Buffer
	RenderGapReportHuman(&b, gr, nil)
	got := b.String()
	if !strings.Contains(got, "AAA") || !strings.Contains(got, "BBB") {
		t.Fatalf("both gap vars must appear:\n%s", got)
	}
	if !strings.Contains(got, "2 gap(s) found") {
		t.Fatalf("summary line missing:\n%s", got)
	}
}

// TestRenderGapReportHuman_NoANSI: nil styler produces no ANSI sequences.
func TestRenderGapReportHuman_NoANSI(t *testing.T) {
	gr := GapReport{Count: 1, Gaps: []GapSite{
		{Var: "WEB_PORT", Service: "web", Field: "ports[0]", Fallback: "0:80"},
	}}
	var b bytes.Buffer
	RenderGapReportHuman(&b, gr, nil)
	if strings.Contains(b.String(), "\x1b[") {
		t.Fatalf("nil styler must produce no ANSI:\n%s", b.String())
	}
}
