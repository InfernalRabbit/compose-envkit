package envmap

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Format selects an env emission encoding for `cenvkit env`.
type Format string

const (
	FormatDotenv Format = "dotenv" // KEY="value"  (round-trips compose-go's dotenv parser)
	FormatJSON   Format = "json"   // a single JSON object {"KEY":"value",...}
	FormatShell  Format = "shell"  // export KEY='value'  (safe to eval)
)

// ParseFormat validates a --format value.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case FormatDotenv, FormatJSON, FormatShell:
		return Format(s), nil
	default:
		return "", fmt.Errorf("invalid --format %q (want dotenv|json|shell)", s)
	}
}

// Emit writes the chain-derived keys of r in the requested format, key-sorted
// (r.ChainKeys is already sorted). Values come from r.Full so a shell override of
// a chain key is reflected. The emitted set is BOUNDED to chain keys (spec §5e) —
// the full inherited process env is never dumped.
//
// All three formats are written to be safe to consume: `dotenv` round-trips
// compose-go's own parser; `shell` is safe to `eval`; `json` is standard JSON.
func Emit(w io.Writer, r Resolved, f Format) error {
	switch f {
	case FormatJSON:
		obj := make(map[string]string, len(r.ChainKeys))
		for _, k := range r.ChainKeys {
			obj[k] = r.Full[k]
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(obj)
	case FormatShell:
		for _, k := range r.ChainKeys {
			if !validShellIdent(k) {
				// A key that is not a valid shell identifier cannot be a safe `export`
				// target; refuse rather than emit something eval would mangle (MF8).
				return fmt.Errorf("key %q is not a valid shell identifier; cannot emit --format shell", k)
			}
			_, _ = fmt.Fprintf(w, "export %s=%s\n", k, shellQuote(r.Full[k]))
		}
		return nil
	case FormatDotenv:
		for _, k := range r.ChainKeys {
			_, _ = fmt.Fprintf(w, "%s=%s\n", k, dotenvQuote(r.Full[k]))
		}
		return nil
	default:
		return fmt.Errorf("invalid format %q", f)
	}
}

// EmitDotenv writes r as dotenv to w. It is the format `cenvkit run --print` uses
// (identical content to `cenvkit env --format dotenv`, spec §5d).
func EmitDotenv(w io.Writer, r Resolved) error {
	return Emit(w, r, FormatDotenv)
}

// shellQuote wraps v in single quotes, escaping any embedded single quote via the
// POSIX '\” idiom (close quote, escaped literal quote, reopen quote). Single
// quotes suppress ALL shell expansion, so the result is safe to eval verbatim.
func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

// dotenvQuote double-quotes v and escapes the characters compose-go's dotenv
// parser treats specially inside a double-quoted value, so the output round-trips
// that parser back to the original value (probe-verified, v2.11.0):
//   - backslash  → \\   (escapes are processed in "...")
//   - "          → \"   (terminates the quoted value otherwise)
//   - $          → \$   (compose-go expands ${VAR}/$VAR inside "..."; \$ is literal)
//   - newline    → \n   (a raw newline would split the line)
//
// Always double-quoting (even plain values) keeps the encoder simple and the
// output unambiguous; the parser strips the quotes on read.
func dotenvQuote(v string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		`$`, `\$`,
		"\n", `\n`,
	)
	return `"` + r.Replace(v) + `"`
}

// validShellIdent reports whether k is a POSIX-ish shell variable name
// ([A-Za-z_][A-Za-z0-9_]*). A dotenv key charset is broader ([A-Za-z0-9_.-]), so a
// key with '.' or '-' (or a leading digit) is rejected for --format shell.
func validShellIdent(k string) bool {
	if k == "" {
		return false
	}
	for i, r := range k {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
			// always allowed
		case r >= '0' && r <= '9':
			if i == 0 {
				return false // a shell identifier may not start with a digit
			}
		default:
			return false
		}
	}
	return true
}
