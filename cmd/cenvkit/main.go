// Command cenvkit assembles COMPOSE_ENV_FILES from a layered env chain and
// execs `docker compose`. See docs/superpowers/specs for the design.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/compose-envkit/compose-envkit/internal/bootstrap"
	"github.com/compose-envkit/compose-envkit/internal/chain"
	"github.com/compose-envkit/compose-envkit/internal/debug"
	"github.com/compose-envkit/compose-envkit/internal/engine"
	"github.com/compose-envkit/compose-envkit/internal/envfiles"
)

// version is overridden at release time via -ldflags "-X main.version=...".
var version = "dev"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "cenvkit",
		Short:         "Layered env-file assembly for Docker Compose",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().String("project-dir", "", "project directory (default: current directory)")
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the cenvkit version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), version)
			return nil
		},
	})
	root.AddCommand(newEnvFilesCmd(), newComposeCmd(), newValidateCmd(),
		newInitCmd(), newEnvDebugCmd())
	// COMPOSE_DEPTH is accepted-but-ignored (spec §5): include-graph load makes it obsolete.
	return root
}

// resolveProjectDir honors --project-dir, defaulting to the current directory.
// --project-dir is a PERSISTENT flag on the root. cmd.Flags() does NOT surface it
// when cmd is the root itself (cobra only merges inherited flags into a
// *subcommand*'s Flags() at parse/Execute time), so reading it that way returns ""
// when resolveProjectDir is called standalone. cmd.Flag() searches the local,
// inherited, and persistent flag sets WITHOUT requiring a merge, so it works for
// every receiver — the root, any subcommand, and the compose-path override
// (newComposeCmd's cmd.Flags().Set writes through to this same shared flag).
func resolveProjectDir(cmd *cobra.Command) (string, error) {
	pd := ""
	if f := cmd.Flag("project-dir"); f != nil {
		pd = f.Value.String()
	}
	if pd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		pd = wd
	}
	return filepath.Abs(pd)
}

// assemble runs Layer-1 + Layer-2 and returns the merged COMPOSE_ENV_FILES list,
// the chain result, and the engine result (engine empty when no compose file).
// envOverlay entries (e.g. "COMPOSE_ENV=prod") are appended AFTER os.Environ() so
// they win via chain's osEnvMap last-wins — this is how `validate --all` re-resolves
// the Layer-1 chain per env (findings [3]/[9]).
func assemble(cmd *cobra.Command, envOverlay ...string) ([]string, chain.Result, engine.Result, error) {
	dir, err := resolveProjectDir(cmd)
	if err != nil {
		return nil, chain.Result{}, engine.Result{}, err
	}
	osEnv := append(os.Environ(), envOverlay...)
	cr, err := chain.Resolve(chain.Input{ProjectDir: dir, OSEnv: osEnv, Hostname: os.Hostname})
	if err != nil {
		return nil, chain.Result{}, engine.Result{}, err
	}
	var er engine.Result
	// HasComposeFileEnv takes the FULL seed env (cr.Vars) so ${COMPOSE_ENV} in a
	// COMPOSE_FILE entry interpolates and COMPOSE_PATH_SEPARATOR is honored — it
	// shares the resolveComposeFiles seam with engine.Resolve, so gate and loader
	// cannot drift (findings [10]/[22]/[23]). cr.Vars carries COMPOSE_ENV.
	if engine.HasComposeFileEnv(dir, cr.Vars) {
		er, err = engine.New().Resolve(context.Background(), engine.Input{
			ProjectDir: dir,
			Env:        cr.Vars,
			Profiles:   splitProfiles(envValue(cr.Vars, "COMPOSE_PROFILES")),
		})
		if err != nil {
			return nil, chain.Result{}, engine.Result{}, err
		}
	}
	merged, err := envfiles.Assemble(cr.Files, er.EnvFiles)
	if err != nil {
		return nil, chain.Result{}, engine.Result{}, err
	}
	return merged, cr, er, nil
}

func envValue(vars []string, key string) string {
	for _, kv := range vars {
		if strings.HasPrefix(kv, key+"=") {
			return kv[len(key)+1:]
		}
	}
	return ""
}

func splitProfiles(v string) []string {
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}

func newEnvFilesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "env-files",
		Short: "Print the resolved COMPOSE_ENV_FILES chain, one path per line",
		RunE: func(cmd *cobra.Command, _ []string) error {
			merged, _, _, err := assemble(cmd)
			if err != nil {
				return err
			}
			for _, f := range merged {
				fmt.Fprintln(cmd.OutOrStdout(), f)
			}
			return nil
		},
	}
}

func newComposeCmd() *cobra.Command {
	c := &cobra.Command{
		Use:                "compose [args...]",
		Short:              "Assemble the chain and exec `docker compose`",
		DisableFlagParsing: true, // pass everything through to docker compose
		RunE: func(cmd *cobra.Command, args []string) error {
			// DisableFlagParsing means cobra does NOT parse the persistent
			// --project-dir here; it leaks into args and `docker compose` would
			// reject it as an unknown flag. Pre-scan args: extract its value to
			// override the project dir, and STRIP every occurrence (both
			// `--project-dir VAL` and `--project-dir=VAL`, in any position) so it
			// is never forwarded to docker compose. (finding [5])
			dirOverride, args := extractProjectDir(args)
			if dirOverride != "" {
				_ = cmd.Flags().Set("project-dir", dirOverride)
			}
			merged, _, _, err := assemble(cmd)
			if err != nil {
				return err
			}
			dir, err := resolveProjectDir(cmd)
			if err != nil {
				return err
			}
			dc := exec.Command("docker", append([]string{"compose"}, args...)...)
			dc.Dir = dir // run where the chain/engine resolved files (lib/compose-env.sh:130-131)
			dc.Env = append(os.Environ(), "COMPOSE_ENV_FILES="+envfiles.Join(merged))
			dc.Stdin, dc.Stdout, dc.Stderr = os.Stdin, os.Stdout, os.Stderr
			if err := dc.Run(); err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					os.Exit(ee.ExitCode())
				}
				return fmt.Errorf("exec docker compose: %w", err)
			}
			return nil
		},
	}
	return c
}

// extractProjectDir pulls --project-dir out of a DisableFlagParsing arg slice,
// supporting `--project-dir VAL` and `--project-dir=VAL` in any position, and
// returns the value (last wins) plus args with all occurrences removed.
func extractProjectDir(args []string) (string, []string) {
	val := ""
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--project-dir":
			if i+1 < len(args) {
				val = args[i+1]
				i++ // skip the value token
			}
		case strings.HasPrefix(a, "--project-dir="):
			val = strings.TrimPrefix(a, "--project-dir=")
		default:
			out = append(out, a)
		}
	}
	return val, out
}

func newValidateCmd() *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use:   "validate",
		Short: "Validate the resolved compose config (docker compose config -q)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			run := func(env string) error {
				var ov []string
				if env != "" {
					ov = []string{"COMPOSE_ENV=" + env} // re-resolve the Layer-1 chain for THIS env
				}
				merged, _, _, err := assemble(cmd, ov...)
				if err != nil {
					return err
				}
				dir, err := resolveProjectDir(cmd)
				if err != nil {
					return err
				}
				dc := exec.Command("docker", "compose", "config", "-q")
				dc.Dir = dir
				dc.Env = append(os.Environ(), "COMPOSE_ENV_FILES="+envfiles.Join(merged))
				if env != "" {
					dc.Env = append(dc.Env, "COMPOSE_ENV="+env) // also render ${COMPOSE_ENV} in compose files
				}
				dc.Stdout, dc.Stderr = os.Stdout, os.Stderr
				return dc.Run()
			}
			if all {
				if err := run("dev"); err != nil {
					return fmt.Errorf("dev config invalid: %w", err)
				}
				return run("prod")
			}
			return run("")
		},
	}
	c.Flags().BoolVar(&all, "all", false, "validate both dev and prod")
	return c
}

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Seed .X from example.X (no-clobber) and fan out one level",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := resolveProjectDir(cmd)
			if err != nil {
				return err
			}
			return bootstrap.Init(dir)
		},
	}
}

func newEnvDebugCmd() *cobra.Command {
	var (
		mChain, mDiff, mEffective, mFiles, mTrace, mValue bool
		varName                                           string
	)
	c := &cobra.Command{
		Use:   "env-debug",
		Short: "Inspect the resolved env chain",
		RunE: func(cmd *cobra.Command, _ []string) error {
			merged, cr, _, err := assemble(cmd)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			switch {
			case mValue:
				fmt.Fprintln(out, debug.Value(cr.Files, varName)) // Layer-1 chain only (smoke.sh:218)
			case mTrace:
				debug.Trace(out, merged, varName) // merged: --trace must see Layer-2 (e.g. SVC_PORT)
			case mFiles:
				debug.PrintFiles(out, merged)
			case mDiff:
				debug.Diff(out, cr.Files, sliceAfter(merged, cr.Files))
			case mEffective:
				dc := exec.Command("docker", "compose", "config")
				dc.Env = append(os.Environ(), "COMPOSE_ENV_FILES="+envfiles.Join(merged))
				dc.Stdout, dc.Stderr = out, os.Stderr
				return dc.Run()
			default: // --chain is the default view
				_ = mChain
				debug.PrintChain(out, cr.Files)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&mChain, "chain", false, "show Layer-1 chain files")
	c.Flags().BoolVar(&mDiff, "diff", false, "show vars Layer-2 adds over Layer-1")
	c.Flags().BoolVar(&mEffective, "effective", false, "docker compose config (rendered)")
	c.Flags().BoolVar(&mFiles, "files", false, "show full COMPOSE_ENV_FILES list")
	c.Flags().BoolVar(&mTrace, "trace", false, "trace files that set --var")
	c.Flags().BoolVar(&mValue, "value", false, "print the effective value of --var")
	c.Flags().StringVar(&varName, "var", "", "variable name for --trace/--value")
	return c
}

// sliceAfter returns merged entries that are not in layer1 (the Layer-2 tail).
func sliceAfter(merged, layer1 []string) []string {
	in := map[string]bool{}
	for _, f := range layer1 {
		in[f] = true
	}
	var out []string
	for _, f := range merged {
		if !in[f] {
			out = append(out, f)
		}
	}
	return out
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "cenvkit:", err)
		os.Exit(1)
	}
}
