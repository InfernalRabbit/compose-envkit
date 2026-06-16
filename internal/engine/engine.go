// Package engine wraps compose-go (v2.11.0). It is the ONLY package in cenvkit
// that imports compose-go; everything else consumes the plain-Go Result/ProjectView.
package engine

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/compose-spec/compose-go/v2/cli"

	"github.com/compose-envkit/compose-envkit/internal/provenance"
)

// Input describes one Layer-2 resolution request.
type Input struct {
	ProjectDir  string   // absolute working dir
	ConfigFiles []string // explicit -f; empty => COMPOSE_FILE / default discovery
	Env         []string // chain.Result.Vars — seeds interpolation
	Profiles    []string // active profiles (M3)
}

// ProjectView is a compose-go-free projection so internal/provenance and cmd
// never import compose-go.
type ProjectView struct {
	WorkingDir string
	Services   map[string][]string // service -> existing resolved env_file abs paths
}

// Result is the active Layer-2 env_file set, deduped & ordered, ready to append
// after Layer-1 into COMPOSE_ENV_FILES.
type Result struct {
	EnvFiles []string // absolute, existing, active-only, deterministically ordered, deduped
	Project  ProjectView
}

// Engine is the seam. One real impl over compose-go; trivially fakeable in tests.
type Engine interface {
	Resolve(ctx context.Context, in Input) (Result, error)
	Provenance(ctx context.Context, in ProvInput) (provenance.Report, error)
}

type composeEngine struct{}

// New returns the compose-go-backed Engine, pinned to v2.11.0.
func New() Engine { return &composeEngine{} }

func (e *composeEngine) Resolve(ctx context.Context, in Input) (Result, error) {
	// COMPOSE_FILE selection + ${VAR} interpolation is the cenvkit-side seam:
	// probe-verified (compose-go v2.11.0, see .claude/artifacts/compose-go-d1-lever.md)
	// that cli.WithConfigFileEnv (a) only sees COMPOSE_FILE if WithEnv ran first,
	// and (b) os.Stats the RAW string with NO ${VAR} interpolation and resolves
	// relative entries against the PROCESS cwd, not WithWorkingDirectory. So when
	// in.ConfigFiles is empty we compute the config list ourselves (interpolated,
	// joined-to-abs against in.ProjectDir) and pass it as the configs arg.
	configs := in.ConfigFiles
	if len(configs) == 0 {
		configs = resolveComposeFiles(in.ProjectDir, in.Env) // nil => default discovery
	}
	opts, err := cli.NewProjectOptions(configs,
		cli.WithWorkingDirectory(in.ProjectDir),
		cli.WithEnv(in.Env),              // FIRST: seeds o.Environment so the options below see it
		cli.WithConfigFileEnv,            // now sees COMPOSE_FILE if set; harmless when configs explicit
		cli.WithDefaultConfigPath,        // default docker-compose.y*ml / compose.y*ml discovery
		cli.WithProfiles(in.Profiles),    // M3 profiles passthrough
		cli.WithResolvedPaths(true),      // EnvFile.Path => absolute
		cli.WithInterpolation(true),      // ${...} in paths resolves from in.Env
		cli.WithoutEnvironmentResolution, // D1 lever: missing required env_file does not abort
	)
	if err != nil {
		return Result{}, fmt.Errorf("compose project options: %w", err)
	}
	proj, err := opts.LoadProject(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load compose project: %w", err)
	}

	view := ProjectView{WorkingDir: proj.WorkingDir, Services: map[string][]string{}}
	var out []string
	seen := map[string]bool{}

	// Deterministic emission: sort service names; preserve env_file order within a service.
	names := make([]string, 0, len(proj.Services))
	for name := range proj.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		svc := proj.Services[name]
		var existing []string
		for _, ef := range svc.EnvFiles {
			if _, statErr := os.Stat(ef.Path); statErr != nil {
				continue // D1 skip-missing parity (compose-go keeps the path; we drop it)
			}
			existing = append(existing, ef.Path)
			if !seen[ef.Path] {
				seen[ef.Path] = true
				out = append(out, ef.Path)
			}
		}
		view.Services[name] = existing
	}
	return Result{EnvFiles: out, Project: view}, nil
}
