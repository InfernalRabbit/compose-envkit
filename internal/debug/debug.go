// Package debug renders env-debug views over the assembled file set and a tiny
// dotenv merge. Pure Go — no compose-go (consumes engine.ProjectView only).
package debug

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// mergeDotEnv reads the given files in order and returns the last-wins effective
// values plus, per variable, the ordered list of files that set it. Values are
// returned as written (no ${...} expansion — env-debug --value is a raw lookup,
// audit Option B).
func mergeDotEnv(files []string) (map[string]string, map[string][]string) {
	vals := map[string]string{}
	sources := map[string][]string{} // var -> files that set it (in order)
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
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
			vals[k] = v // last wins
			sources[k] = append(sources[k], f)
		}
		fh.Close()
	}
	return vals, sources
}

// Value returns the effective (last-wins) value of name across files; "" if unset.
func Value(files []string, name string) string {
	vals, _ := mergeDotEnv(files)
	return vals[name]
}

// PrintChain writes the Layer-1 file list, one per line.
func PrintChain(w io.Writer, layer1 []string) {
	for _, f := range layer1 {
		fmt.Fprintln(w, f)
	}
}

// PrintFiles writes the full assembled COMPOSE_ENV_FILES list, one per line.
func PrintFiles(w io.Writer, all []string) {
	for _, f := range all {
		fmt.Fprintln(w, f)
	}
}

// Trace shows which files set name and the effective value.
func Trace(w io.Writer, files []string, name string) {
	vals, sources := mergeDotEnv(files)
	for _, src := range sources[name] {
		fmt.Fprintf(w, "%s\t%s\n", name, src)
	}
	fmt.Fprintf(w, "effective %s=%s\n", name, vals[name])
}

// Diff lists variables contributed by Layer-2 that are not present in Layer-1.
// Output is sorted by variable name for deterministic, diffable output.
func Diff(w io.Writer, layer1, layer2 []string) {
	l1, _ := mergeDotEnv(layer1)
	l2, _ := mergeDotEnv(layer2)
	keys := make([]string, 0, len(l2))
	for k := range l2 {
		if _, inL1 := l1[k]; !inL1 {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "+ %s=%s\n", k, l2[k])
	}
}
