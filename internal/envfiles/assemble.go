// Package envfiles merges the Layer-1 chain and the Layer-2 enumerated set into
// the final ordered COMPOSE_ENV_FILES list. Pure Go.
package envfiles

import (
	"fmt"
	"strings"
)

// Assemble returns Layer-1 (in chain order, secrets last by construction) followed
// by Layer-2, with any path present in both emitted once in its Layer-1 position.
// Variable precedence is last-wins by this order at docker-compose load time.
func Assemble(layer1, layer2 []string) ([]string, error) {
	out := make([]string, 0, len(layer1)+len(layer2))
	seen := map[string]bool{}
	add := func(p string) error {
		if strings.ContainsRune(p, ',') {
			return fmt.Errorf("env file path %q contains a comma (COMPOSE_ENV_FILES separator)", p)
		}
		if seen[p] {
			return nil
		}
		seen[p] = true
		out = append(out, p)
		return nil
	}
	for _, p := range layer1 {
		if err := add(p); err != nil {
			return nil, err
		}
	}
	for _, p := range layer2 {
		if err := add(p); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Join renders the assembled list as a COMPOSE_ENV_FILES value.
func Join(files []string) string { return strings.Join(files, ",") }
