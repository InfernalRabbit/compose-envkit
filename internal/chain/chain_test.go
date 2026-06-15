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
	writeFile(t, dir, ".docker-env-chain", ".env\n.${HOST}.env\n.secrets.env\n")
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
	// The resolved file must be .evlhost.env and exist; no entry may contain a comma.
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
	writeFile(t, dir, ".docker-env-chain", ".env\n.${ENV}.env\n")
	writeFile(t, dir, ".env", "BASE=1\n")
	writeFile(t, dir, ".ab.env", "X=1\n") // "a,b" sanitized -> "ab"
	res, err := Resolve(Input{
		ProjectDir: dir,
		OSEnv:      []string{"COMPOSE_ENV=a,b"},
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
	writeFile(t, dir, ".docker-env-chain", ".env\n.${ENV}.env\n.${HOSTNAME}.env\n.secrets.env\n")
	writeFile(t, dir, ".env", "COMPOSE_ENV=dev\nROOT=1\n")
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
		{"prod via shell", []string{"COMPOSE_ENV=prod", "HOSTNAME=testhost"},
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

func TestHostTokenEqualsHostnameToken(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".docker-env-chain", ".env\n.${HOST}.env\n")
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
