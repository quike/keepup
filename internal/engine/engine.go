// Package engine orchestrates the execution of keepup groups in declarative steps.
package engine

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/logger"
)

// Engine runs a configuration step-by-step. Groups within a step run in
// parallel up to MaxConcurrency. Output from any group is visible to later
// steps via the OutputStore.
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

// Outputs returns the captured outputs (after Run completes).
func (e *Engine) Outputs() OutputStore { return e.outputs }

// Run executes every step in order, respecting ctx cancellation. The first
// failure within a step cancels its siblings; subsequent steps are skipped.
func (e *Engine) Run(ctx context.Context) error {
	for stepIndex, step := range e.cfg.Execution {
		e.log.Info("starting step", "step", stepIndex+1, "groups", step.Group)

		// Validate all referenced groups before launching any goroutines so a
		// typo never fires a partial step.
		for _, name := range step.Group {
			if _, ok := e.groups[name]; !ok {
				return fmt.Errorf("step %d: group %q not defined", stepIndex+1, name)
			}
		}

		if err := e.runStep(ctx, step); err != nil {
			return fmt.Errorf("step %d: %w", stepIndex+1, err)
		}
		e.log.Info("step completed", "step", stepIndex+1)
	}
	return nil
}

func (e *Engine) runStep(ctx context.Context, step config.Step) error {
	// Snapshot outputs once per step so all parallel groups in this step see
	// the same baseline (outputs of prior steps), not racy mid-step writes.
	baseline := e.outputs.Snapshot()

	g, gctx := errgroup.WithContext(ctx)
	if e.maxConcurrency > 0 {
		g.SetLimit(e.maxConcurrency)
	}

	for _, name := range step.Group {
		group := e.groups[name]
		g.Go(func() error {
			return e.runGroup(gctx, &group, baseline)
		})
	}
	return g.Wait()
}

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
