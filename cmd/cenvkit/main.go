// Command cenvkit assembles COMPOSE_ENV_FILES from a layered env chain and
// execs `docker compose`. See docs/superpowers/specs for the design.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/InfernalRabbit/compose-envkit/internal/bootstrap"
	"github.com/InfernalRabbit/compose-envkit/internal/chain"
	"github.com/InfernalRabbit/compose-envkit/internal/engine"
	"github.com/InfernalRabbit/compose-envkit/internal/envfiles"
	"github.com/InfernalRabbit/compose-envkit/internal/envmap"
	"github.com/InfernalRabbit/compose-envkit/internal/provenance"
	"github.com/InfernalRabbit/compose-envkit/internal/style"
)

// version is overridden at release time via -ldflags "-X main.version=...".
var version = "dev"

// composeGoVersion returns the linked compose-go module version read at runtime
// from the binary's build info. Returns "" if build info is unavailable (e.g.
// in some test harnesses) or the dep is not found.
func composeGoVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, dep := range bi.Deps {
		if dep.Path == "github.com/compose-spec/compose-go/v2" {
			return dep.Version
		}
	}
	return ""
}

// resolveVersion returns a meaningful version string for any build path:
//   - ldflags-injected (release or `make install` git-describe stamp): returned as-is.
//   - `go install module@vX` / `@latest`: bi.Main.Version is a real or pseudo-version.
//   - plain `go build`: vcs.revision from build settings gives dev+<commit>[-dirty].
//   - no build info: falls back to the bare "dev" default.
func resolveVersion() string {
	if version != "dev" {
		return version // release build or Makefile git-describe stamp; do not override
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version // go install module@vX or @latest
	}
	// Plain `go build`: derive from VCS info embedded by the toolchain.
	var rev, modified string
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if rev == "" {
		return version
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	v := "dev+" + rev
	if modified == "true" {
		v += "-dirty"
	}
	return v
}

// styler is resolved once in the root PersistentPreRunE from --color and used by
// every subcommand for human output. currentStyler() is nil-safe so a subcommand
// invoked without the root pre-run (e.g. a direct unit test) still gets plain.
var styler provenance.Styler

func currentStyler() provenance.Styler {
	if styler == nil {
		return style.Disabled()
	}
	return styler
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "cenvkit",
		Short:         "Layered env-file assembly for Docker Compose",
		SilenceUsage:  true,
		SilenceErrors: true,
		// Resolve ONE styler for the whole invocation from --color (+ NO_COLOR /
		// CLICOLOR_FORCE / TTY, handled by termenv). Stored in a package var so every
		// subcommand uses the same decision; gated on os.Stdout (human output). The
		// --json path swaps in style.Disabled() at its own call site (top precedence).
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			flag := "auto"
			if f := cmd.Root().PersistentFlags().Lookup("color"); f != nil {
				flag = f.Value.String()
			}
			styler = style.Resolve(flag, os.Stdout)
			return nil
		},
	}
	root.PersistentFlags().String("project-dir", "", "project directory (default: current directory)")
	root.PersistentFlags().String("color", "auto", "colorize output: auto|always|never")
	// --chain selects a named [name] section from .cenvkit.envchain (C4). It is
	// persistent (mirrors --project-dir) so every chain-reading subcommand inherits
	// ONE definition via resolveChainName; "" / "default" = the implicit
	// header-less/[default] section. Commands that never read the chain (version,
	// init) inherit it harmlessly, exactly as they inherit --project-dir.
	root.PersistentFlags().String("chain", "", "named chain section from .cenvkit.envchain (default: the header-less/[default] chain)")
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the cenvkit version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintln(out, resolveVersion())
			if cgv := composeGoVersion(); cgv != "" {
				_, _ = fmt.Fprintln(out, "compose-go "+cgv)
			}
			return nil
		},
	})
	root.AddCommand(newEnvFilesCmd(), newComposeCmd(), newValidateCmd(),
		newInitCmd(), newEnvDebugCmd(), newGapReportCmd(),
		newRunCmd(), newEnvCmd())
	// COMPOSE_DEPTH is accepted-but-ignored (spec §5): include-graph load makes it obsolete.
	return root
}

// exitError carries a process exit code out of a command's RunE so main() can
// os.Exit with it. An empty msg means the command already wrote its own output
// (e.g. the gap report) and only the code should propagate.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }
func (e *exitError) ExitCode() int { return e.code }

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

// resolveChainName reads the persistent --chain flag (C4 named-chain selector),
// mirroring resolveProjectDir. cmd.Flag() searches local/inherited/persistent
// sets without a merge, so it works for the root, any subcommand, and the compose
// pre-scan override (which writes through to this same shared flag). "" means the
// implicit header-less/[default] chain — chain.Resolve normalizes it.
func resolveChainName(cmd *cobra.Command) string {
	if f := cmd.Flag("chain"); f != nil {
		return f.Value.String()
	}
	return ""
}

// assemble resolves the Layer-1 chain and returns the COMPOSE_ENV_FILES list and
// the chain result. v3 (Layer-2 debug-only): the run path is Layer-1 ONLY — a
// service `env_file:` is runtime-only (native per-container) and is deliberately
// NOT folded into COMPOSE_ENV_FILES. So we pass nil as the Layer-2 arg and skip
// engine.Resolve entirely (D1: faster compose/env-files; the engine's Layer-2
// enumeration now lives only in env-debug as a gap-detector).
//
// envOverlay entries (e.g. "CENVKIT_ENV=prod") are appended AFTER os.Environ() so
// they win via chain's osEnvMap last-wins — this is how `validate --all` re-resolves
// the Layer-1 chain per env (findings [3]/[9]).
func assemble(cmd *cobra.Command, envOverlay ...string) ([]string, chain.Result, error) {
	dir, err := resolveProjectDir(cmd)
	if err != nil {
		return nil, chain.Result{}, err
	}
	osEnv := append(os.Environ(), envOverlay...)
	cr, err := chain.Resolve(chain.Input{ProjectDir: dir, OSEnv: osEnv, Hostname: os.Hostname, Chain: resolveChainName(cmd)})
	if err != nil {
		return nil, chain.Result{}, err
	}
	merged, err := envfiles.Assemble(cr.Files, nil)
	if err != nil {
		return nil, chain.Result{}, err
	}
	return merged, cr, nil
}

// resolvePopulator is the shared front half of `run` and `env`: resolve the
// project dir, build the process env (os.Environ plus an optional `-e ENV`
// overlay), resolve the Layer-1 chain for THAT env, and flatten it into a merged
// shell-wins environment via internal/envmap.
//
// The `-e` overlay is a "CENVKIT_ENV=<v>" entry appended AFTER os.Environ() so it
// wins via chain's osEnvMap last-wins (the same mechanism `validate --all` uses,
// see newValidateCmd). It feeds BOTH the chain selection (which `.${ENV}.env` is
// picked) AND the flatten base, so the two stay consistent. expand toggles
// ${VAR} expansion (--expand default; --no-expand literal).
func resolvePopulator(cmd *cobra.Command, env string, expand bool) (envmap.Resolved, error) {
	dir, err := resolveProjectDir(cmd)
	if err != nil {
		return envmap.Resolved{}, err
	}
	osEnv := os.Environ()
	if env != "" {
		osEnv = append(osEnv, "CENVKIT_ENV="+env) // re-resolve the chain for this env; wins (last)
	}
	cr, err := chain.Resolve(chain.Input{ProjectDir: dir, OSEnv: osEnv, Hostname: os.Hostname, Chain: resolveChainName(cmd)})
	if err != nil {
		return envmap.Resolved{}, err
	}
	// envmap is fed ONLY the existence-filtered chain list (chain.Resolve drops
	// missing paths, chain.go:177-178), so engine.Flatten's error-on-missing never
	// fires for a legitimately-absent chain slot. processEnv = the same osEnv used
	// for selection, so the flatten base and the shell-wins overlay match it.
	return envmap.Resolve(envStringSliceToMap(osEnv), cr.Files, expand)
}

// envStringSliceToMap turns os.Environ()-style "K=V" entries into a map. Later
// entries win (so an appended "-e" overlay overrides an inherited value).
func envStringSliceToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i > 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
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
			s := currentStyler()
			for _, f := range merged {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), s.Path(f)) // cyan on a TTY; plain when piped
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
			// DisableFlagParsing means cobra does NOT parse cenvkit's own persistent
			// flags here; they leak into args and `docker compose` would reject them
			// as unknown. Pre-scan args: extract each cenvkit flag's value to override
			// the shared persistent flag, and STRIP every occurrence (both `--flag VAL`
			// and `--flag=VAL`, in any position) so it is never forwarded to docker
			// compose. --project-dir is finding [5]; --chain is the C4 named-chain
			// selector (it must reach chain.Resolve via the persistent flag, not docker).
			dirOverride, args := extractPersistentFlag(args, "project-dir")
			if dirOverride != "" {
				_ = cmd.Flags().Set("project-dir", dirOverride)
			}
			chainOverride, args := extractPersistentFlag(args, "chain")
			if chainOverride != "" {
				_ = cmd.Flags().Set("chain", chainOverride)
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

// extractPersistentFlag pulls a `--<name>` valued flag out of a DisableFlagParsing
// arg slice, supporting `--name VAL` and `--name=VAL` in any position, and returns
// the value (last wins) plus args with all occurrences removed. Used by the
// compose pass-through to strip cenvkit's own persistent flags (--project-dir,
// --chain) before forwarding the rest to `docker compose`.
func extractPersistentFlag(args []string, name string) (string, []string) {
	long := "--" + name
	eq := long + "="
	val := ""
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == long:
			if i+1 < len(args) {
				val = args[i+1]
				i++ // skip the value token
			}
		case strings.HasPrefix(a, eq):
			val = strings.TrimPrefix(a, eq)
		default:
			out = append(out, a)
		}
	}
	return val, out
}

// extractProjectDir is the --project-dir specialization of extractPersistentFlag,
// retained as a named seam for the existing unit test.
func extractProjectDir(args []string) (string, []string) {
	return extractPersistentFlag(args, "project-dir")
}

func newValidateCmd() *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use:   "validate",
		Short: "Validate the resolved compose config (docker compose config -q)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s := currentStyler()
			run := func(env string) error {
				var ov []string
				if env != "" {
					ov = []string{"CENVKIT_ENV=" + env} // re-resolve the Layer-1 chain for THIS env
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
					dc.Env = append(dc.Env, "CENVKIT_ENV="+env) // also render ${CENVKIT_ENV} in compose files
				}
				dc.Stdout, dc.Stderr = os.Stdout, os.Stderr
				if err := dc.Run(); err != nil {
					return err // exit code preserved; main prints the error red
				}
				label := "config valid"
				if env != "" {
					label = env + " config valid"
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), s.Ok(label))
				return nil
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
		mList, mEffective, mFiles, mTrace, mValue, mOverview, jsonOut bool
		varName, service                                              string
	)
	c := &cobra.Command{
		Use:   "env-debug",
		Short: "Inspect the resolved env chain with provenance",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := resolveProjectDir(cmd)
			if err != nil {
				return err
			}
			cr, err := chain.Resolve(chain.Input{ProjectDir: dir, OSEnv: os.Environ(), Hostname: os.Hostname, Chain: resolveChainName(cmd)})
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
				// Top precedence (spec §5 rule 0): JSON is NEVER colored. RenderJSON
				// emits no styling, so the machine output stays ANSI-free regardless
				// of --color=always.
				return provenance.RenderJSON(out, rep)
			}
			provenance.RenderHuman(out, rep, provenance.HumanOpts{
				Trace: pick(mTrace, varName), Value: pick(mValue, varName),
				Effective: mEffective, Service: service, Chain: mList, Files: mFiles,
				Overview:         mOverview,
				ComposeEnv:       cr.ComposeEnv,
				ComposeEnvSource: cr.ComposeEnvSource,
				ProjectDir:       dir,
				Style:            currentStyler(),
			})
			return nil
		},
	}
	// --list is this command's default view (the Layer-1 chain-file list). It was
	// named --chain pre-C4; C4 reclaims the universal string selector `--chain
	// <name>` (the persistent root flag) for picking a named [name] section, so the
	// boolean mode was renamed --list to avoid a same-flag string/bool collision.
	c.Flags().BoolVar(&mList, "list", false, "Layer-1 chain files (default view)")
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

// newGapReportCmd is the daemon-free CI/pre-build lint over the EXISTING gap set
// (engine.Provenance's VarTrace.Gap, internal/engine/provenance.go:424). It loads
// the compose model in process — never execs docker — and maps the result to a
// distinct exit-code contract via exitError: gaps found => 1, clean => 0, no
// compose file discovered => 2 (a misconfiguration, NOT a clean pass; spec §6).
func newGapReportCmd() *cobra.Command {
	var jsonOut bool
	c := &cobra.Command{
		Use:   "gap-report",
		Short: "Report env_file:->${VAR} interpolation gaps (CI/pre-build lint; daemon-free)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := resolveProjectDir(cmd)
			if err != nil {
				return err
			}
			cr, err := chain.Resolve(chain.Input{ProjectDir: dir, OSEnv: os.Environ(), Hostname: os.Hostname, Chain: resolveChainName(cmd)})
			if err != nil {
				return err
			}
			// No compose file => the lint cannot run; for a pre-build check that is a
			// misconfiguration, NOT a clean pass. Exit 2 (distinct from 1=gaps/0=clean).
			if !engine.HasComposeFileEnv(dir, cr.Vars) {
				return &exitError{code: 2, msg: "no compose file found in " + dir}
			}
			pf := make([]engine.ProvFile, 0, len(cr.Files))
			for _, f := range cr.Files {
				pf = append(pf, engine.ProvFile{Path: f, Layer: "layer1"})
			}
			profiles := splitProfiles(envValue(cr.Vars, "COMPOSE_PROFILES"))
			rep, err := engine.New().Provenance(cmd.Context(), engine.ProvInput{
				ProjectDir: dir, Env: cr.Vars, Profiles: profiles, EnvFiles: pf,
			})
			if err != nil {
				return err
			}
			gr := provenance.CollectGaps(rep)
			out := cmd.OutOrStdout()
			if jsonOut {
				if err := provenance.RenderGapReportJSON(out, gr); err != nil {
					return err
				}
			} else {
				provenance.RenderGapReportHuman(out, gr, currentStyler())
			}
			if gr.Count > 0 {
				return &exitError{code: 1} // gaps found; report already printed (no extra msg)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&jsonOut, "json", false, "machine-readable JSON")
	return c
}

// newEnvCmd emits the chain-derived environment (the no-docker "local arm").
// It prints ONLY the keys the chain contributed, key-sorted, with the shell
// overlaid (shell-wins) — it never dumps the whole inherited process env. An
// empty/all-missing chain prints nothing and exits 0 (a fresh repo is legitimate).
func newEnvCmd() *cobra.Command {
	var (
		envSel   string
		noExpand bool
		format   string
	)
	c := &cobra.Command{
		Use:   "env",
		Short: "Print the chain-derived environment (dotenv|json|shell)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f, err := envmap.ParseFormat(format)
			if err != nil {
				return err
			}
			res, err := resolvePopulator(cmd, envSel, !noExpand)
			if err != nil {
				return err
			}
			return envmap.Emit(cmd.OutOrStdout(), res, f)
		},
	}
	c.Flags().StringVarP(&envSel, "env", "e", "", "select the chain env (overrides CENVKIT_ENV); e.g. -e prod")
	c.Flags().Bool("expand", true, "expand ${VAR} in chain values (default)")
	c.Flags().BoolVar(&noExpand, "no-expand", false, "emit chain values literally, leaving ${VAR} unexpanded")
	c.MarkFlagsMutuallyExclusive("expand", "no-expand")
	c.Flags().StringVar(&format, "format", "dotenv", "output format: dotenv|json|shell")
	return c
}

// newRunCmd flattens the chain and execs a command with the merged environment
// (shell-wins). `--` is REQUIRED so cenvkit's own flags never collide with the
// child's; everything after `--` is the command, passed verbatim. `--print` dumps
// the chain-derived env (dotenv) and exits 0 WITHOUT exec'ing.
func newRunCmd() *cobra.Command {
	var (
		envSel   string
		noExpand bool
		print    bool
	)
	c := &cobra.Command{
		Use:                   "run [-e ENV] [--expand|--no-expand] [--print] -- <cmd> [args...]",
		Short:                 "Exec a command with the chain-derived environment",
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// `--` enforcement: ArgsLenAtDash() is the count of args BEFORE `--` when
			// present, and -1 when absent (cobra v1.10.2, probe-verified). args already
			// has everything before `--` stripped and flag-parsing stops at `--`, so a
			// child flag like `run -- echo --foo` passes `--foo` through untouched.
			if cmd.ArgsLenAtDash() < 0 {
				return &exitError{code: 2, msg: "run requires `--` before the command (e.g. cenvkit run -- npm test)"}
			}
			if len(args) == 0 {
				return &exitError{code: 2, msg: "no command after `--`"}
			}
			res, err := resolvePopulator(cmd, envSel, !noExpand)
			if err != nil {
				return err
			}
			if print {
				// --print: dump the chain-derived env (identical to `env --format
				// dotenv`), skip exec, exit 0. Reveals plaintext secrets by design.
				return envmap.EmitDotenv(cmd.OutOrStdout(), res)
			}
			return execChild(args, res.Full)
		},
	}
	c.Flags().StringVarP(&envSel, "env", "e", "", "select the chain env (overrides CENVKIT_ENV); e.g. -e prod")
	c.Flags().Bool("expand", true, "expand ${VAR} in chain values (default)")
	c.Flags().BoolVar(&noExpand, "no-expand", false, "emit chain values literally, leaving ${VAR} unexpanded")
	c.MarkFlagsMutuallyExclusive("expand", "no-expand")
	c.Flags().BoolVar(&print, "print", false, "print the chain-derived env (dotenv) and exit; do not exec")
	return c
}

// execChild runs args[0] with args[1:] and the given merged environment,
// forwarding stdio and signals, and maps the outcome to a POSIX-parity exit code
// via exitError (MF6):
//   - clean exit            → the child's own code (0 = nil error)
//   - child exits non-zero  → *exec.ExitError → its ExitCode()
//   - child killed by signal → 128 + signo
//   - command not found      → 127
//   - found but not executable → 126
//
// We use exec.Command (not syscall.Exec) so --print can run earlier without an
// ordering trap and so signal forwarding + exit mapping are explicit and testable.
func execChild(args []string, env map[string]string) error {
	bin := args[0]
	// Resolve PATH ourselves so a missing binary maps to 127 and a non-executable
	// file maps to 126 BEFORE we try to start it (exec.Command defers lookup to
	// Start, conflating the two). LookPath returns exec.ErrNotFound / a permission
	// error we can distinguish.
	if _, err := exec.LookPath(bin); err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return &exitError{code: 127, msg: bin + ": command not found"}
		}
		if errors.Is(err, os.ErrPermission) {
			return &exitError{code: 126, msg: bin + ": permission denied"}
		}
		return &exitError{code: 127, msg: bin + ": " + err.Error()}
	}

	c := exec.Command(bin, args[1:]...)
	c.Env = mapToEnvSlice(env)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr

	// Forward terminating signals to the child so Ctrl-C etc. reaches it; the
	// child's own signal-terminated status then drives our 128+signo exit below.
	// Scoped to the real terminating set (NOT a bare Notify) so we don't relay
	// runtime-internal signals like SIGURG/SIGCHLD that Go uses.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	defer signal.Stop(sigCh)

	if err := c.Start(); err != nil {
		// Start failed after LookPath succeeded (rare: file changed under us).
		if errors.Is(err, os.ErrPermission) {
			return &exitError{code: 126, msg: bin + ": permission denied"}
		}
		return &exitError{code: 127, msg: bin + ": " + err.Error()}
	}
	go func() {
		for s := range sigCh {
			_ = c.Process.Signal(s)
		}
	}()

	err := c.Wait()
	if err == nil {
		return nil // child exited 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return &exitError{code: 128 + int(ws.Signal())} // signal-terminated; report already on stderr
		}
		return &exitError{code: ee.ExitCode()} // ordinary non-zero exit
	}
	return &exitError{code: 1, msg: "run " + bin + ": " + err.Error()}
}

// mapToEnvSlice turns a merged env map into a sorted os.Environ()-style slice for
// exec. Sorting is purely for determinism (stable test goldens / reproducible
// process inspection); the OS does not care about order.
func mapToEnvSlice(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+m[k])
	}
	return out
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		// Errors go to stderr in red (spec §4). Gate on STDERR's TTY (not stdout,
		// which the PersistentPreRun styler used) and honor --color via a best-effort
		// args scan — the pre-run may not have run if parsing itself failed.
		es := style.Resolve(colorFlagFromArgs(os.Args), os.Stderr)
		// exitError carries a command's own exit code (e.g. gap-report 1/2). An empty
		// msg means the command already wrote its output; only the code propagates.
		var ee *exitError
		if errors.As(err, &ee) {
			if ee.msg != "" {
				_, _ = fmt.Fprintln(os.Stderr, es.ErrorMsg("cenvkit: "+ee.msg))
			}
			os.Exit(ee.code)
		}
		// A --chain <name> that the chain file doesn't define is a usage error: exit 2
		// (distinct from the generic exit 1), with the available chain names already in
		// the error message (chain.UnknownChainError.Error()).
		var uce *chain.UnknownChainError
		if errors.As(err, &uce) {
			_, _ = fmt.Fprintln(os.Stderr, es.ErrorMsg("cenvkit: "+err.Error()))
			os.Exit(2)
		}
		_, _ = fmt.Fprintln(os.Stderr, es.ErrorMsg("cenvkit: "+err.Error()))
		os.Exit(1)
	}
}

// colorFlagFromArgs best-effort-extracts the --color value from raw args so error
// coloring honors --color even when cobra parsing failed before PersistentPreRunE.
// Defaults to "auto". Accepts `--color X` and `--color=X`.
func colorFlagFromArgs(args []string) string {
	for i, a := range args {
		switch {
		case a == "--color":
			if i+1 < len(args) {
				return args[i+1]
			}
		case strings.HasPrefix(a, "--color="):
			return strings.TrimPrefix(a, "--color=")
		}
	}
	return "auto"
}
