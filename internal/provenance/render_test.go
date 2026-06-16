package provenance

import (
	"bytes"
	"strings"
	"testing"
)

func sampleReport() Report {
	return Report{
		Files: []string{"/p/.env", "/p/.secrets.env", "/p/web/.web.env"},
		Vars: map[string]VarTrace{
			"APP_PORT": {
				Name: "APP_PORT", Value: "8080",
				Winner:     Source{File: "/p/web/.web.env", Layer: "layer2"},
				Overridden: []Source{{File: "/p/.env", Layer: "layer1"}},
				Effects:    []Effect{{Service: "web", Field: "ports[0]", Resolved: "8080:80"}},
			},
		},
		Services: []ServiceEnv{{
			Service: "web",
			Entries: []EnvEntry{{Key: "APP_PORT", Value: "8080", Source: Source{File: "/p/web/.web.env", Layer: "env_file"}}},
		}},
	}
}

func TestRenderJSON_Stable(t *testing.T) {
	var b bytes.Buffer
	if err := RenderJSON(&b, sampleReport()); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{`"name": "APP_PORT"`, `"winner"`, `"field": "ports[0]"`, `"resolved": "8080:80"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("JSON missing %q:\n%s", want, got)
		}
	}
}

func TestRenderHuman_TraceShowsWinnerAndEffects(t *testing.T) {
	var b bytes.Buffer
	RenderHuman(&b, sampleReport(), HumanOpts{Trace: "APP_PORT"})
	got := b.String()
	for _, want := range []string{"APP_PORT=8080", "web/.web.env", "ports[0]", "8080:80"} {
		if !strings.Contains(got, want) {
			t.Fatalf("human --trace missing %q:\n%s", want, got)
		}
	}
}
