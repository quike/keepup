// Package engine orchestrates the execution of a keepup Flow. Groups are
// scheduled either as ordered waves (step mode) or topologically from the
// implicit data DAG (dag mode); both share the same Runner, OutputStore,
// Expander, and Logger interfaces.
package engine

import (
	"context"
	"fmt"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/logger"
	"github.com/quike/keepup/internal/plan"
)

// Engine binds a parsed Config to a set of pluggable collaborators.
type Engine struct {
	cfg            *config.Config
	groups         map[string]config.Group
	runner         Runner
	outputs        OutputStore
	expander       Expander
	log            logger.Logger
	maxConcurrency int
	dryRun         bool
}

// Option configures an Engine.
type Option func(*Engine)

// WithRunner overrides the default ShellRunner.
func WithRunner(r Runner) Option { return func(e *Engine) { e.runner = r } }

// WithOutputStore overrides the default in-memory OutputStore.
func WithOutputStore(s OutputStore) Option { return func(e *Engine) { e.outputs = s } }

// WithExpander overrides the default TemplateExpander.
func WithExpander(x Expander) Option { return func(e *Engine) { e.expander = x } }

// WithLogger overrides the default no-op Logger.
func WithLogger(l logger.Logger) Option { return func(e *Engine) { e.log = l } }

// WithDryRun forces dry-run mode regardless of the config flag.
func WithDryRun(dry bool) Option { return func(e *Engine) { e.dryRun = dry } }

// New constructs an Engine. The config pointer is held by reference; do not
// mutate it for the lifetime of the Engine.
func New(cfg *config.Config, opts ...Option) *Engine {
	groups := make(map[string]config.Group, len(cfg.Groups))
	for _, g := range cfg.Groups {
		groups[g.Name] = g
	}
	e := &Engine{
		cfg:            cfg,
		groups:         groups,
		runner:         NewShellRunner(),
		outputs:        NewMemoryOutputStore(),
		expander:       TemplateExpander{},
		log:            logger.Nop(),
		maxConcurrency: cfg.Settings.MaxConcurrency,
		dryRun:         cfg.Settings.DryRun,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Outputs returns the captured outputs (populated after RunFlow completes).
func (e *Engine) Outputs() OutputStore { return e.outputs }

// RunFlow executes the named Flow, honoring ctx cancellation.
func (e *Engine) RunFlow(ctx context.Context, flowName string) error {
	if flowName == "" {
		if e.cfg.Default == "" {
			return fmt.Errorf("no flow specified and no default declared")
		}
		flowName = e.cfg.Default
	}
	p, err := plan.Build(e.cfg, flowName)
	if err != nil {
		return err
	}
	e.log.Info("starting flow", "flow", flowName, "mode", string(p.Mode))

	switch p.Mode {
	case config.ModeStep:
		return e.runStepPlan(ctx, p)
	case config.ModeDAG:
		return e.runDAGPlan(ctx, p)
	default:
		return fmt.Errorf("flow %q: unknown mode %q", flowName, p.Mode)
	}
}

// runGroup expands a group's params against the supplied output baseline,
// invokes the Runner (unless dry-run is set), and records the captured output.
func (e *Engine) runGroup(ctx context.Context, group *config.Group, baseline map[string]string) error {
	expanded := make([]string, len(group.Params))
	for i, p := range group.Params {
		expanded[i] = e.expander.Expand(p, baseline)
	}
	if e.dryRun {
		e.log.Info("[dry-run] would run",
			"group", group.Name, "command", group.Command, "params", expanded, "shell", group.UseShell())
		return nil
	}
	e.log.Info("running group", "group", group.Name, "command", group.Command, "params", expanded)
	out, err := e.runner.Run(ctx, group, expanded, e.cfg.Env)
	if err != nil {
		e.log.Error("group failed", "group", group.Name, "err", err.Error(), "output", out)
		return err
	}
	e.log.Trace("group output", "group", group.Name, "output", out)
	e.outputs.Set(group.Name, out)
	return nil
}
