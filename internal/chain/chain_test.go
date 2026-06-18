package chain

import (
	"os"
	"path/filepath"
	"strings" // used by TestChainOrderingAndEnvSwitch; declared here so Step 6 compiles
	"testing"
)

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHostSanitization_NoInjectionNoSplit(t *testing.T) {
	dir := t.TempDir()
	// chain references a host-named file; the resolved name must be sanitized.
	writeFile(t, dir, ".cenvkit.envchain", ".env\n.${HOST}.env\n.secrets.env\n")
	writeFile(t, dir, ".env", "BASE=1\n")
	writeFile(t, dir, ".evlhost.env", "H=1\n") // sanitized target
	writeFile(t, dir, ".secrets.env", "S=1\n")

	res, err := Resolve(Input{
		ProjectDir: dir,
		OSEnv:      []string{"HOSTNAME=ev|l&host"}, // sed-special chars
		Hostname:   func() (string, error) { return "ev|l&host", nil },
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Host != "evlhost" {
		t.Fatalf("Host = %q, want %q", res.Host, "evlhost")
	}
	// The resolved file must be .evlhost.env and exist; no entry may contain a comma
	// (a comma would corrupt the COMPOSE_ENV_FILES separator — that var is Docker's own).
	wantHostFile := filepath.Join(dir, ".evlhost.env")
	found := false
	for _, f := range res.Files {
		if f == wantHostFile {
			found = true
		}
		for _, c := range f {
			if c == ',' {
				t.Fatalf("resolved path contains comma (would split COMPOSE_ENV_FILES): %q", f)
			}
		}
	}
	if !found {
		t.Fatalf("did not resolve .evlhost.env; Files=%v", res.Files)
	}
}

func TestCommaInComposeEnv_DoesNotSplit(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".cenvkit.envchain", ".env\n.${ENV}.env\n")
	writeFile(t, dir, ".env", "BASE=1\n")
	writeFile(t, dir, ".ab.env", "X=1\n") // "a,b" sanitized -> "ab"
	res, err := Resolve(Input{
		ProjectDir: dir,
		OSEnv:      []string{"CENVKIT_ENV=a,b"},
		Hostname:   func() (string, error) { return "h", nil },
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.ComposeEnv != "ab" {
		t.Fatalf("ComposeEnv sanitized = %q, want %q", res.ComposeEnv, "ab")
	}
}

func TestChainOrderingAndEnvSwitch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".cenvkit.envchain", ".env\n.${ENV}.env\n.${HOSTNAME}.env\n.secrets.env\n")
	writeFile(t, dir, ".env", "CENVKIT_ENV=dev\nROOT=1\n")
	writeFile(t, dir, ".dev.env", "TIER=dev\n")
	writeFile(t, dir, ".prod.env", "TIER=prod\n")
	writeFile(t, dir, ".testhost.env", "H=1\n")
	writeFile(t, dir, ".secrets.env", "SECRET=xyz\n")

	tests := []struct {
		name      string
		osEnv     []string
		wantOrder []string // basenames, in order
		wantEnv   string
	}{
		{"dev default", []string{"HOSTNAME=testhost"},
			[]string{".env", ".dev.env", ".testhost.env", ".secrets.env"}, "dev"},
		{"prod via shell", []string{"CENVKIT_ENV=prod", "HOSTNAME=testhost"},
			[]string{".env", ".prod.env", ".testhost.env", ".secrets.env"}, "prod"},
		{"non-matching host drops .testhost.env", []string{"HOSTNAME=otherhost"},
			[]string{".env", ".dev.env", ".secrets.env"}, "dev"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Resolve(Input{ProjectDir: dir, OSEnv: tc.osEnv,
				Hostname: func() (string, error) { return "fallback", nil }})
			if err != nil {
				t.Fatal(err)
			}
			if res.ComposeEnv != tc.wantEnv {
				t.Fatalf("ComposeEnv=%q want %q", res.ComposeEnv, tc.wantEnv)
			}
			var got []string
			for _, f := range res.Files {
				got = append(got, filepath.Base(f))
			}
			if strings.Join(got, ",") != strings.Join(tc.wantOrder, ",") {
				t.Fatalf("order=%v want %v", got, tc.wantOrder)
			}
			// secrets must be last whenever present
			if got[len(got)-1] != ".secrets.env" {
				t.Fatalf("secrets not last: %v", got)
			}
		})
	}
}

// TestResolveComposeEnvSource: the three branches of resolveComposeEnv
// (chain.go:92-103) correctly tag ComposeEnvSource as "shell" | ".env" |
// "default", and ComposeEnv carries the corresponding resolved value.
// RED on a mislabel (e.g. "shell" when reading from .env, or "default" when
// the shell has the var set).
func TestResolveComposeEnvSource(t *testing.T) {
	tests := []struct {
		name           string
		osEnv          []string // injected OS environment
		dotEnvBody     string   // body for the root .env (empty = do not create)
		wantComposeEnv string
		wantSource     string
	}{
		{
			name:           "shell CENVKIT_ENV → source=shell",
			osEnv:          []string{"CENVKIT_ENV=staging"},
			dotEnvBody:     "CENVKIT_ENV=dev\n", // .env also has it; shell wins
			wantComposeEnv: "staging",
			wantSource:     "shell",
		},
		{
			name:           "no shell, CENVKIT_ENV in .env → source=.env",
			osEnv:          []string{},
			dotEnvBody:     "CENVKIT_ENV=prod\n",
			wantComposeEnv: "prod",
			wantSource:     ".env",
		},
		{
			name:           "no shell, no .env → source=default",
			osEnv:          []string{},
			dotEnvBody:     "", // no .env created
			wantComposeEnv: "dev",
			wantSource:     "default",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.dotEnvBody != "" {
				writeFile(t, dir, ".env", tc.dotEnvBody)
			}
			res, err := Resolve(Input{
				ProjectDir: dir,
				OSEnv:      tc.osEnv,
				Hostname:   func() (string, error) { return "testhost", nil },
			})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if res.ComposeEnv != tc.wantComposeEnv {
				t.Fatalf("ComposeEnv = %q, want %q", res.ComposeEnv, tc.wantComposeEnv)
			}
			if res.ComposeEnvSource != tc.wantSource {
				t.Fatalf("ComposeEnvSource = %q, want %q", res.ComposeEnvSource, tc.wantSource)
			}
		})
	}
}

func TestHostTokenEqualsHostnameToken(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".cenvkit.envchain", ".env\n.${HOST}.env\n")
	writeFile(t, dir, ".env", "B=1\n")
	writeFile(t, dir, ".testhost.env", "H=1\n")
	res, err := Resolve(Input{ProjectDir: dir, OSEnv: []string{"HOSTNAME=testhost"},
		Hostname: func() (string, error) { return "x", nil }})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(res.Files[len(res.Files)-1]) != ".testhost.env" {
		t.Fatalf("${HOST} did not resolve to .testhost.env: %v", res.Files)
	}
}
