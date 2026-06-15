package main

import (
	"bytes"
	"path/filepath"
	"testing"
)

// version subcommand output via the cobra OutOrStdout() wiring (spec §5).
func TestVersionSubcommand(t *testing.T) {
	root := newRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := bytes.TrimSpace(buf.Bytes()); string(got) != version {
		t.Fatalf("version output = %q, want %q", got, version)
	}
}

// --project-dir is registered AND its set-branch flows through resolveProjectDir
// (the acceptance suite drives scope via cwd and never sets this flag, so the
// flag-set branch is otherwise dead).
func TestProjectDirFlagWiring(t *testing.T) {
	root := newRootCmd()
	if root.PersistentFlags().Lookup("project-dir") == nil {
		t.Fatal("--project-dir persistent flag not registered")
	}
	tmp := t.TempDir()
	if err := root.PersistentFlags().Set("project-dir", tmp); err != nil {
		t.Fatal(err)
	}
	got, err := resolveProjectDir(root)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.Abs(tmp)
	if got != want {
		t.Fatalf("resolveProjectDir=%q want %q (flag-set branch)", got, want)
	}
}

// extractProjectDir strips --project-dir in both forms from a DisableFlagParsing
// arg slice and returns the value + cleaned args.
func TestExtractProjectDir(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantVal  string
		wantArgs []string
	}{
		{"space form", []string{"--project-dir", "/foo", "config"}, "/foo", []string{"config"}},
		{"equals form", []string{"--project-dir=/bar", "config"}, "/bar", []string{"config"}},
		{"last wins", []string{"--project-dir=/a", "--project-dir=/b", "up"}, "/b", []string{"up"}},
		{"absent", []string{"config", "--services"}, "", []string{"config", "--services"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			val, args := extractProjectDir(tc.args)
			if val != tc.wantVal {
				t.Fatalf("val=%q want %q", val, tc.wantVal)
			}
			if len(args) != len(tc.wantArgs) {
				t.Fatalf("args=%v want %v", args, tc.wantArgs)
			}
			for i := range args {
				if args[i] != tc.wantArgs[i] {
					t.Fatalf("args[%d]=%q want %q", i, args[i], tc.wantArgs[i])
				}
			}
		})
	}
}
