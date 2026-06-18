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

// ─── C4: named-chain section tests ───────────────────────────────────────────

// TestNamedChain_HeaderlessIsDefault: a flat (no [section] headers) .cenvkit.envchain
// is fully backward-compatible — it resolves as the "default" chain. Both
// Input.Chain="" and Input.Chain="default" must return the same file list.
// RED if a header-less file is rejected or returns different results for the two selectors.
func TestNamedChain_HeaderlessIsDefault(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".cenvkit.envchain", ".env\n.${ENV}.env\n.secrets.env\n")
	writeFile(t, dir, ".env", "BASE=1\n")
	writeFile(t, dir, ".dev.env", "TIER=dev\n")
	writeFile(t, dir, ".secrets.env", "S=1\n")

	osEnv := []string{"CENVKIT_ENV=dev", "HOSTNAME=h"}
	hn := func() (string, error) { return "h", nil }

	resEmpty, err := Resolve(Input{ProjectDir: dir, OSEnv: osEnv, Hostname: hn, Chain: ""})
	if err != nil {
		t.Fatalf("Chain=\"\": %v", err)
	}
	resDefault, err := Resolve(Input{ProjectDir: dir, OSEnv: osEnv, Hostname: hn, Chain: "default"})
	if err != nil {
		t.Fatalf("Chain=\"default\": %v", err)
	}
	if len(resEmpty.Files) == 0 {
		t.Fatal("header-less file with Chain=\"\": expected non-empty file list")
	}
	if len(resEmpty.Files) != len(resDefault.Files) {
		t.Fatalf("Chain=\"\" files=%v != Chain=\"default\" files=%v", resEmpty.Files, resDefault.Files)
	}
	for i := range resEmpty.Files {
		if resEmpty.Files[i] != resDefault.Files[i] {
			t.Fatalf("file[%d]: Chain=\"\"=%q != Chain=\"default\"=%q", i, resEmpty.Files[i], resDefault.Files[i])
		}
	}
}

// TestNamedChain_StandaloneNoInheritance: a [name] section is COMPLETE and
// STANDALONE — it does NOT inherit entries from [default]. Only the files listed
// directly under [api] must appear; the [default] files must not.
// RED if the [api] result contains any file from [default].
func TestNamedChain_StandaloneNoInheritance(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".cenvkit.envchain",
		".env\n.${ENV}.env\n\n[api]\n.api.env\n.api.${ENV}.env\n")
	writeFile(t, dir, ".env", "BASE=1\n")
	writeFile(t, dir, ".dev.env", "TIER=dev\n")
	writeFile(t, dir, ".api.env", "API=1\n")
	writeFile(t, dir, ".api.dev.env", "API_TIER=dev\n")

	osEnv := []string{"CENVKIT_ENV=dev", "HOSTNAME=h"}
	hn := func() (string, error) { return "h", nil }

	res, err := Resolve(Input{ProjectDir: dir, OSEnv: osEnv, Hostname: hn, Chain: "api"})
	if err != nil {
		t.Fatalf("Resolve [api]: %v", err)
	}

	// Only api files must appear — default files (.env, .dev.env) must be absent.
	for _, f := range res.Files {
		base := filepath.Base(f)
		if base == ".env" || base == ".dev.env" {
			t.Fatalf("[api] chain inherited default file %q; sections must be standalone", base)
		}
	}
	// Both api files must appear.
	bases := map[string]bool{}
	for _, f := range res.Files {
		bases[filepath.Base(f)] = true
	}
	for _, want := range []string{".api.env", ".api.dev.env"} {
		if !bases[want] {
			t.Fatalf("[api] chain missing expected file %q; got %v", want, res.Files)
		}
	}
}

// TestNamedChain_MissingSectionError: requesting a [name] that is not in the
// file must return an *UnknownChainError listing the available named sections
// (not including "default"). RED if a nil error is returned or if the available
// list is missing or wrong.
func TestNamedChain_MissingSectionError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".cenvkit.envchain",
		".env\n\n[api]\n.api.env\n\n[web]\n.web.env\n")
	writeFile(t, dir, ".env", "BASE=1\n")

	_, err := Resolve(Input{
		ProjectDir: dir,
		OSEnv:      []string{"CENVKIT_ENV=dev"},
		Hostname:   func() (string, error) { return "h", nil },
		Chain:      "typo",
	})
	if err == nil {
		t.Fatal("expected error for missing chain \"typo\", got nil")
	}
	uce, ok := err.(*UnknownChainError)
	if !ok {
		t.Fatalf("expected *UnknownChainError, got %T: %v", err, err)
	}
	if uce.Name != "typo" {
		t.Fatalf("UnknownChainError.Name=%q want %q", uce.Name, "typo")
	}
	// Available must list the named sections (sorted), not "default".
	avail := map[string]bool{}
	for _, n := range uce.Available {
		avail[n] = true
	}
	if avail["default"] {
		t.Fatalf("Available must not include %q (it always resolves): %v", "default", uce.Available)
	}
	for _, want := range []string{"api", "web"} {
		if !avail[want] {
			t.Fatalf("Available missing %q: %v", want, uce.Available)
		}
	}
}

// TestNamedChain_DefaultValidBothWays: a file with named sections still allows
// selecting "default" — both Chain="" and Chain="default" must resolve to the
// header-less preamble entries, not the named sections.
// RED if Chain="default" returns an error or returns a named-section file.
func TestNamedChain_DefaultValidBothWays(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".cenvkit.envchain",
		".env\n.${ENV}.env\n\n[api]\n.api.env\n")
	writeFile(t, dir, ".env", "BASE=1\n")
	writeFile(t, dir, ".dev.env", "TIER=dev\n")
	// .api.env intentionally not created — must not appear in default

	osEnv := []string{"CENVKIT_ENV=dev", "HOSTNAME=h"}
	hn := func() (string, error) { return "h", nil }

	for _, sel := range []string{"", "default"} {
		res, err := Resolve(Input{ProjectDir: dir, OSEnv: osEnv, Hostname: hn, Chain: sel})
		if err != nil {
			t.Fatalf("Chain=%q: unexpected error: %v", sel, err)
		}
		for _, f := range res.Files {
			if filepath.Base(f) == ".api.env" {
				t.Fatalf("Chain=%q: default section must not include .api.env", sel)
			}
		}
		// .env and .dev.env must be present
		bases := map[string]bool{}
		for _, f := range res.Files {
			bases[filepath.Base(f)] = true
		}
		for _, want := range []string{".env", ".dev.env"} {
			if !bases[want] {
				t.Fatalf("Chain=%q: default section missing %q; got %v", sel, want, res.Files)
			}
		}
	}
}

// TestNamedChain_TokensSubstituteInSection: ${ENV}, ${CENVKIT_ENV}, ${HOST},
// ${HOSTNAME} must be substituted within a named section's entries just as they
// are in the default section. RED if the resolved file list contains a literal
// unsubstituted token or if the substituted file is absent.
func TestNamedChain_TokensSubstituteInSection(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".cenvkit.envchain",
		"[svc]\n.svc.env\n.svc.${ENV}.env\n.${HOSTNAME}.svc.env\n")
	writeFile(t, dir, ".svc.env", "SVC=1\n")
	writeFile(t, dir, ".svc.prod.env", "SVC_TIER=prod\n")
	writeFile(t, dir, ".myhost.svc.env", "SVC_HOST=1\n")

	res, err := Resolve(Input{
		ProjectDir: dir,
		OSEnv:      []string{"CENVKIT_ENV=prod", "HOSTNAME=myhost"},
		Hostname:   func() (string, error) { return "myhost", nil },
		Chain:      "svc",
	})
	if err != nil {
		t.Fatalf("Resolve [svc]: %v", err)
	}

	bases := map[string]bool{}
	for _, f := range res.Files {
		base := filepath.Base(f)
		if strings.Contains(base, "${") {
			t.Fatalf("unsubstituted token in resolved path: %q", base)
		}
		bases[base] = true
	}
	for _, want := range []string{".svc.env", ".svc.prod.env", ".myhost.svc.env"} {
		if !bases[want] {
			t.Fatalf("[svc] chain missing expected file %q; got %v", want, res.Files)
		}
	}
}

// TestNamedChain_ExplicitDefaultHeaderConcatenates: an explicit [default] header
// in the middle of the file concatenates with pre-header lines into ONE default
// list — the pre-header block and the [default]-headed block are the same section.
// RED if the explicit [default] header starts a fresh/empty section or if the
// pre-header lines are silently dropped.
func TestNamedChain_ExplicitDefaultHeaderConcatenates(t *testing.T) {
	dir := t.TempDir()
	// Pre-header line .env, then explicit [default] with .dev.env — both in default.
	writeFile(t, dir, ".cenvkit.envchain",
		".env\n\n[default]\n.dev.env\n\n[api]\n.api.env\n")
	writeFile(t, dir, ".env", "BASE=1\n")
	writeFile(t, dir, ".dev.env", "TIER=dev\n")
	writeFile(t, dir, ".api.env", "API=1\n")

	res, err := Resolve(Input{
		ProjectDir: dir,
		OSEnv:      []string{"CENVKIT_ENV=dev", "HOSTNAME=h"},
		Hostname:   func() (string, error) { return "h", nil },
		Chain:      "default",
	})
	if err != nil {
		t.Fatalf("Resolve default: %v", err)
	}
	bases := map[string]bool{}
	for _, f := range res.Files {
		bases[filepath.Base(f)] = true
	}
	// Both pre-header AND post-[default]-header entries must appear.
	for _, want := range []string{".env", ".dev.env"} {
		if !bases[want] {
			t.Fatalf("explicit [default] header: expected %q in default list; got %v", want, res.Files)
		}
	}
	// [api] entries must NOT appear in the default list.
	if bases[".api.env"] {
		t.Fatalf("explicit [default] header: .api.env must not appear in default list; got %v", res.Files)
	}
}

// TestNamedChain_EmptyDefaultExitsZero: selecting [default] from a file whose
// default section has no entries (e.g. file opens directly with [api]) must return
// an empty file list and exit 0 — not an error. An empty chain is legitimate.
// RED if an error is returned or if the file list is non-nil from named sections.
func TestNamedChain_EmptyDefaultExitsZero(t *testing.T) {
	dir := t.TempDir()
	// File with no pre-header lines; [default] section is empty by design.
	writeFile(t, dir, ".cenvkit.envchain", "[api]\n.api.env\n")

	res, err := Resolve(Input{
		ProjectDir: dir,
		OSEnv:      []string{"CENVKIT_ENV=dev", "HOSTNAME=h"},
		Hostname:   func() (string, error) { return "h", nil },
		Chain:      "default",
	})
	if err != nil {
		t.Fatalf("empty default must not error; got: %v", err)
	}
	if len(res.Files) != 0 {
		t.Fatalf("empty default chain must have no files; got %v", res.Files)
	}
}

// TestNamedChain_EmptyBracketKey: a literal `[]` line in the chain file creates a
// section keyed "" (empty string). Requesting Chain="" maps to "default", NOT to
// the empty-key section — the empty section is unreachable and harmless.
// RED if Chain="" returns the `[]` section's entries instead of the default entries.
func TestNamedChain_EmptyBracketKey(t *testing.T) {
	dir := t.TempDir()
	// Pre-header .env belongs to default; [] section has .secret.env; [api] has .api.env.
	writeFile(t, dir, ".cenvkit.envchain", ".env\n\n[]\n.secret.env\n\n[api]\n.api.env\n")
	writeFile(t, dir, ".env", "BASE=1\n")
	writeFile(t, dir, ".secret.env", "S=1\n")

	res, err := Resolve(Input{
		ProjectDir: dir,
		OSEnv:      []string{"CENVKIT_ENV=dev", "HOSTNAME=h"},
		Hostname:   func() (string, error) { return "h", nil },
		Chain:      "", // empty → default
	})
	if err != nil {
		t.Fatalf("Chain=\"\" with [] present must not error; got: %v", err)
	}
	bases := map[string]bool{}
	for _, f := range res.Files {
		bases[filepath.Base(f)] = true
	}
	// Only the pre-header .env must appear; [] section's .secret.env must NOT.
	if !bases[".env"] {
		t.Fatalf("Chain=\"\" must resolve to default (.env); got %v", res.Files)
	}
	if bases[".secret.env"] {
		t.Fatalf("Chain=\"\" must NOT reach [] section's .secret.env; got %v", res.Files)
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
