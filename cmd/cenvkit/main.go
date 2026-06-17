// Command cenvkit assembles COMPOSE_ENV_FILES from a layered env chain and
// execs `docker compose`. See docs/superpowers/specs for the design.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/InfernalRabbit/compose-envkit/internal/bootstrap"
	"github.com/InfernalRabbit/compose-envkit/internal/chain"
	"github.com/InfernalRabbit/compose-envkit/internal/engine"
	"github.com/InfernalRabbit/compose-envkit/internal/envfiles"
	"github.com/InfernalRabbit/compose-envkit/internal/provenance"
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

// assemble resolves the Layer-1 chain and returns the COMPOSE_ENV_FILES list and
// the chain result. v3 (Layer-2 debug-only): the run path is Layer-1 ONLY — a
// service `env_file:` is runtime-only (native per-container) and is deliberately
// NOT folded into COMPOSE_ENV_FILES. So we pass nil as the Layer-2 arg and skip
// engine.Resolve entirely (D1: faster compose/env-files; the engine's Layer-2
// enumeration now lives only in env-debug as a gap-detector).
//
// envOverlay entries (e.g. "COMPOSE_ENV=prod") are appended AFTER os.Environ() so
// they win via chain's osEnvMap last-wins — this is how `validate --all` re-resolves
// the Layer-1 chain per env (findings [3]/[9]).
func assemble(cmd *cobra.Command, envOverlay ...string) ([]string, chain.Result, error) {
	dir, err := resolveProjectDir(cmd)
	if err != nil {
		return nil, chain.Result{}, err
	}
	osEnv := append(os.Environ(), envOverlay...)
	cr, err := chain.Resolve(chain.Input{ProjectDir: dir, OSEnv: osEnv, Hostname: os.Hostname})
	if err != nil {
		return nil, chain.Result{}, err
	}
	merged, err := envfiles.Assemble(cr.Files, nil)
	if err != nil {
		return nil, chain.Result{}, err
	}
	return merged, cr, nil
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
			merged, _, err := assemble(cmd)
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
			merged, _, err := assemble(cmd)
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
				merged, _, err := assemble(cmd, ov...)
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
		mChain, mEffective, mFiles, mTrace, mValue, mOverview, jsonOut bool
		varName, service                                               string
	)
	c := &cobra.Command{
		Use:   "env-debug",
		Short: "Inspect the resolved env chain with provenance",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := resolveProjectDir(cmd)
			if err != nil {
				return err
			}
			cr, err := chain.Resolve(chain.Input{ProjectDir: dir, OSEnv: os.Environ(), Hostname: os.Hostname})
			if err != nil {
				return err
			}
			// v3 (Layer-2 debug-only): pass ONLY the Layer-1 chain as ProvFiles. The
			// run's COMPOSE_ENV_FILES is Layer-1-only, so the interpolation env
			// engine.Provenance builds is Layer-1-only too — a ${VAR} defined only in
			// a service env_file: resolves to its run fallback (the gap). The engine
			// still enumerates the active Layer-2 set INTERNALLY (its C-loop, the
			// gap-detector evidence); we no longer call engine.Resolve here (drops the
			// redundant load — D1).
			pf := make([]engine.ProvFile, 0, len(cr.Files))
			for _, f := range cr.Files {
				pf = append(pf, engine.ProvFile{Path: f, Layer: "layer1"})
			}
			profiles := splitProfiles(envValue(cr.Vars, "COMPOSE_PROFILES"))
			rep, err := engine.New().Provenance(cmd.Context(), engine.ProvInput{
				ProjectDir: dir, Env: cr.Vars, Profiles: profiles, EnvFiles: pf,
				WantLayers: mOverview, // only the --overview path builds the raw layer dump (D-A)
			})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if jsonOut {
				return provenance.RenderJSON(out, rep)
			}
			provenance.RenderHuman(out, rep, provenance.HumanOpts{
				Trace: pick(mTrace, varName), Value: pick(mValue, varName),
				Effective: mEffective, Service: service, Chain: mChain, Files: mFiles,
				Overview:         mOverview,
				ComposeEnv:       cr.ComposeEnv,
				ComposeEnvSource: cr.ComposeEnvSource,
				ProjectDir:       dir,
			})
			return nil
		},
	}
	c.Flags().BoolVar(&mChain, "chain", false, "Layer-1 chain files (default view)")
	c.Flags().BoolVar(&mEffective, "effective", false, "per-service env with sources")
	c.Flags().BoolVar(&mFiles, "files", false, "interpolation set (COMPOSE_ENV_FILES) + runtime-only service env_file paths")
	c.Flags().BoolVar(&mTrace, "trace", false, "trace --var: winner, overridden, effects")
	c.Flags().BoolVar(&mValue, "value", false, "winning value of --var")
	c.Flags().BoolVar(&mOverview, "overview", false, "per-file layering overview (raw values, +/~/· markers)")
	c.Flags().StringVar(&varName, "var", "", "variable for --trace/--value")
	c.Flags().StringVar(&service, "service", "", "filter --effective to one service")
	c.Flags().BoolVar(&jsonOut, "json", false, "machine-readable JSON")
	return c
}

// pick returns name when enabled, else "" — maps a bool mode + --var into the
// string fields provenance.HumanOpts uses for --trace / --value.
func pick(enabled bool, name string) string {
	if enabled {
		return name
	}
	return ""
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "cenvkit:", err)
		os.Exit(1)
	}
}
