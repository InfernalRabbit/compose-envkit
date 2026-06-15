// Package bootstrap implements `cenvkit init`: seed .<X> from example.<X>
// no-clobber, fanning out one directory level. No sudo, no chmod 777, never
// overwrites an existing file (secret-wipe guard).
package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Init seeds the given directory and its immediate subdirectories.
func Init(dir string) error {
	if err := seedDir(dir); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if e.Type()&os.ModeSymlink != 0 {
			continue // skip symlinks (parity with init.sh)
		}
		if err := seedDir(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// seedDir copies each example.<X> to .<X> when the target does not already exist.
func seedDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "example") {
			continue
		}
		target := strings.TrimPrefix(e.Name(), "example") // example.env -> .env
		if target == "" || target == e.Name() {
			continue
		}
		dst := filepath.Join(dir, target)
		if _, err := os.Stat(dst); err == nil {
			continue // no-clobber
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat %s: %w", dst, err)
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dst, err)
		}
	}
	return nil
}
