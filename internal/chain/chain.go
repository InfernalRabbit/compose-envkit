// Package chain resolves Layer-1: the .docker-env-chain file list plus the
// "K=V" seed environment for the engine. Pure Go — imports no compose-go.
package chain

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Input describes one Layer-1 resolution request. OSEnv and Hostname are
// injected for testability; production passes os.Environ() and os.Hostname.
type Input struct {
	ProjectDir string                 // absolute project directory
	OSEnv      []string               // os.Environ(); injected for testability
	Hostname   func() (string, error) // injected; production passes os.Hostname
}

// Result is the resolved Layer-1 view.
type Result struct {
	Files            []string // ordered absolute Layer-1 paths, existing only, deduped
	Vars             []string // merged "K=V" seed for the engine (OS env wins over file vars), sorted
	ComposeEnv       string   // resolved COMPOSE_ENV ("dev" default)
	ComposeEnvSource string   // where ComposeEnv came from: "shell" | ".env" | "default" (for the --overview header)
	Host             string   // resolved + sanitized host ([A-Za-z0-9._-])
}

// defaultChain is used when no .docker-env-chain file is present (spec §4 step 2).
var defaultChain = []string{".env", ".${COMPOSE_ENV}.env", ".secrets.env"}

// sanitizeToken keeps only [A-Za-z0-9._-]; everything else is dropped. This kills
// the legacy sed-injection class and prevents a "," (the COMPOSE_ENV_FILES
// separator) or path-traversal char from entering a resolved path (audit W1).
func sanitizeToken(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	return b.String()
}

func osEnvMap(osEnv []string) map[string]string {
	m := make(map[string]string, len(osEnv))
	for _, kv := range osEnv {
		if i := strings.IndexByte(kv, '='); i > 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

// parseDotEnv is a minimal KEY=VALUE reader (skip blank / #comment lines; strip a
// single pair of surrounding quotes). The authoritative parse happens later in
// compose-go; this only seeds interpolation, so it is intentionally small.
func parseDotEnv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.IndexByte(line, '=')
		if i <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:i])
		v := strings.TrimSpace(line[i+1:])
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		out[k] = v
	}
	return out, sc.Err()
}

// resolveComposeEnv returns the resolved COMPOSE_ENV and the source it came from
// ("shell" | ".env" | "default") for the --overview header (decision §8a).
func resolveComposeEnv(in Input, osEnv map[string]string) (value, source string) {
	if v := osEnv["COMPOSE_ENV"]; v != "" {
		return sanitizeToken(v), "shell"
	}
	// fall back to a COMPOSE_ENV= line in the root .env
	if m, err := parseDotEnv(filepath.Join(in.ProjectDir, ".env")); err == nil {
		if v := m["COMPOSE_ENV"]; v != "" {
			return sanitizeToken(v), ".env"
		}
	}
	return "dev", "default"
}

func resolveHost(in Input, osEnv map[string]string) string {
	if v := osEnv["HOSTNAME"]; v != "" {
		return sanitizeToken(v)
	}
	if in.Hostname != nil {
		if h, err := in.Hostname(); err == nil {
			return sanitizeToken(h)
		}
	}
	if h, err := os.Hostname(); err == nil {
		return sanitizeToken(h)
	}
	return ""
}

func substituteTokens(tmpl, composeEnv, host string) string {
	r := strings.NewReplacer(
		"${ENV}", composeEnv,
		"${COMPOSE_ENV}", composeEnv,
		"${HOST}", host,
		"${HOSTNAME}", host,
	)
	return r.Replace(tmpl)
}

func readChainTemplates(projectDir string) ([]string, error) {
	f, err := os.Open(filepath.Join(projectDir, ".docker-env-chain"))
	if os.IsNotExist(err) {
		return append([]string(nil), defaultChain...), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read .docker-env-chain: %w", err)
	}
	defer f.Close()
	var tmpls []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tmpls = append(tmpls, line)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan .docker-env-chain: %w", err)
	}
	return tmpls, nil
}

// Resolve computes the Layer-1 file list and the seed environment.
func Resolve(in Input) (Result, error) {
	osEnv := osEnvMap(in.OSEnv)
	composeEnv, composeEnvSource := resolveComposeEnv(in, osEnv)
	host := resolveHost(in, osEnv)

	tmpls, err := readChainTemplates(in.ProjectDir)
	if err != nil {
		return Result{}, err
	}

	var files []string
	seen := map[string]bool{}
	fileVars := map[string]string{}
	for _, t := range tmpls {
		name := substituteTokens(t, composeEnv, host)
		if strings.ContainsRune(name, ',') {
			return Result{}, fmt.Errorf("resolved chain entry %q contains a comma (COMPOSE_ENV_FILES separator)", name)
		}
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(in.ProjectDir, name)
		}
		if _, statErr := os.Stat(path); statErr != nil {
			continue // skip-missing parity with the sh kit
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		files = append(files, path)
		if m, perr := parseDotEnv(path); perr == nil {
			for k, v := range m { // later files win (chain order)
				fileVars[k] = v
			}
		}
	}

	// Build Vars: file vars first, then OS env overlays (shell wins).
	merged := map[string]string{}
	for k, v := range fileVars {
		merged[k] = v
	}
	for k, v := range osEnv {
		merged[k] = v
	}
	if _, ok := merged["COMPOSE_ENV"]; !ok {
		merged["COMPOSE_ENV"] = composeEnv
	}
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	vars := make([]string, 0, len(keys))
	for _, k := range keys {
		vars = append(vars, k+"="+merged[k])
	}

	return Result{Files: files, Vars: vars, ComposeEnv: composeEnv, ComposeEnvSource: composeEnvSource, Host: host}, nil
}
