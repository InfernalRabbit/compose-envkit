package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func touch(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHasComposeFile(t *testing.T) {
	tests := []struct {
		name    string
		present []string // files to create in the temp dir
		cfEnv   string   // COMPOSE_FILE value ("" = unset => standard-name discovery)
		want    bool
	}{
		{"unset + standard name present", []string{"compose.yaml"}, "", true},
		{"unset + no standard name", nil, "", false},
		{"explicit a.yml present", []string{"a.yml"}, "a.yml", true},
		{"explicit a.yml missing", nil, "a.yml", false},
		{"colon list, only first exists", []string{"a.yml"}, "a.yml:b.yml", true},
		{"empty value falls to discovery (none) => false", nil, "", false},
		// token-only entry must interpolate before stat (RED on raw-stat impl):
		{"interpolated-only overlay exists", []string{"docker-compose.prod.yml"},
			"docker-compose.${CENVKIT_ENV}.yml", true},
		// comma must NOT be treated as a separator (RED on the deleted heuristic):
		{"comma-joined is a single (nonexistent) path", []string{"a.yml", "b.yml"},
			"a.yml,b.yml", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tc.present {
				touch(t, dir, f)
			}
			// Pass the full seed env so ${CENVKIT_ENV} interpolates.
			env := []string{"CENVKIT_ENV=prod"}
			if tc.cfEnv != "" {
				env = append(env, "COMPOSE_FILE="+tc.cfEnv)
			}
			got := len(resolveComposeFiles(dir, env)) > 0
			if got != tc.want {
				t.Fatalf("resolveComposeFiles present=%v cfEnv=%q: got %v want %v", tc.present, tc.cfEnv, got, tc.want)
			}
		})
	}
}
