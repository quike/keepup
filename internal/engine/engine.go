// Package engine orchestrates the execution of a keepup Flow. Groups are
// scheduled either as ordered waves (step mode) or topologically from the
// implicit data DAG (dag mode); both share the same Runner, OutputStore,
// Expander, and Logger interfaces.
package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/quike/keepup/internal/cache"
	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/logger"
	"github.com/quike/keepup/internal/plan"
)

// Engine binds a parsed Config to a set of pluggable collaborators.
type Engine struct {
	cfg            *config.Config
	groups         map[string]config.Group
	runner         Runner
	prober         Prober
	outputs        OutputStore
	expander       Expander
	cache          cache.Store
	log            logger.Logger
	maxConcurrency int
	dryRun         bool
	noCache        bool
}

// Option configures an Engine.
type Option func(*Engine)

// WithRunner overrides the default ShellRunner.
func WithRunner(r Runner) Option { return func(e *Engine) { e.runner = r } }

// WithProber overrides the default ShellProber used for skip-if/require.
func WithProber(p Prober) Option { return func(e *Engine) { e.prober = p } }

// WithOutputStore overrides the default in-memory OutputStore.
func WithOutputStore(s OutputStore) Option { return func(e *Engine) { e.outputs = s } }

// WithExpander overrides the default TemplateExpander.
func WithExpander(x Expander) Option { return func(e *Engine) { e.expander = x } }

// WithCache overrides the default file-backed cache store.
func WithCache(s cache.Store) Option { return func(e *Engine) { e.cache = s } }

// WithNoCache disables cache reads/writes for this run.
func WithNoCache(disable bool) Option { return func(e *Engine) { e.noCache = disable } }

// WithLogger overrides the default no-op Logger.
func WithLogger(l logger.Logger) Option { return func(e *Engine) { e.log = l } }

// WithDryRun forces dry-run mode regardless of the config flag.
func WithDryRun(dry bool) Option { return func(e *Engine) { e.dryRun = dry } }

// New constructs an Engine. The config pointer is held by reference; do not
// mutate it for the lifetime of the Engine.
func New(cfg *config.Config, opts ...Option) *Engine {
	groups := make(map[string]config.Group, len(cfg.Groups))
	for i := range cfg.Groups {
		groups[cfg.Groups[i].Name] = cfg.Groups[i]
	}
	cacheDir := cfg.Settings.CacheDir
	if cacheDir == "" {
		cacheDir = config.DefaultCacheDir
	}
	e := &Engine{
		cfg:            cfg,
		groups:         groups,
		runner:         NewShellRunner(),
		prober:         ShellProber{},
		outputs:        NewMemoryOutputStore(),
		expander:       TemplateExpander{},
		cache:          cache.NewFileStore(cacheDir),
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

// runGroup decides whether and how to execute a group. The decision order is:
//
//  1. dry-run    → log intent, do nothing else (gating/cache are not evaluated)
//  2. require    → predicate must succeed, else hard error
//  3. skip-if    → predicate success short-circuits the group
//  4. cache      → fingerprint match (with writes present) replays stored output
//  5. otherwise  → invoke the runner and persist output (+ cache entry)
//
// Skipped or cache-hit groups still publish an output value so downstream
// {{ output.X }} references resolve.
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

	if group.Require != "" {
		if err := e.prober.Probe(ctx, group.Require, e.cfg.Env); err != nil {
			return fmt.Errorf("group %q: requirement %q not met: %w", group.Name, group.Require, err)
		}
	}

	if group.SkipIf != "" {
		if err := e.prober.Probe(ctx, group.SkipIf, e.cfg.Env); err == nil {
			out := e.cachedOutput(group)
			e.outputs.Set(group.Name, out)
			e.log.Info("group skipped", "group", group.Name, "reason", "skip-if", "predicate", group.SkipIf)
			return nil
		}
	}

	if fp, hit := e.cacheLookup(group, expanded); hit {
		e.outputs.Set(group.Name, fp.Output)
		e.log.Info("cache hit", "group", group.Name, "fingerprint", fp.Fingerprint)
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
	e.cacheStore(group, expanded, out)
	return nil
}

// cacheLookup returns the stored entry when caching is enabled for the group,
// the fingerprint matches, and all declared writes still exist.
func (e *Engine) cacheLookup(group *config.Group, params []string) (*cache.Entry, bool) {
	if e.noCache || group.Cache == nil {
		return nil, false
	}
	fp, err := cache.Compute(group.Cache, group.Command, params)
	if err != nil {
		e.log.Warn("cache fingerprint failed; running group", "group", group.Name, "err", err.Error())
		return nil, false
	}
	entry, ok := e.cache.Load(group.Name)
	if !ok || entry.Fingerprint != fp {
		return nil, false
	}
	if !cache.WritesPresent(group.Cache) {
		return nil, false
	}
	return entry, true
}

// cacheStore persists a fresh cache entry after a successful run.
func (e *Engine) cacheStore(group *config.Group, params []string, out string) {
	if e.noCache || group.Cache == nil {
		return
	}
	fp, err := cache.Compute(group.Cache, group.Command, params)
	if err != nil {
		e.log.Warn("cache fingerprint failed; not caching", "group", group.Name, "err", err.Error())
		return
	}
	entry := &cache.Entry{
		Fingerprint: fp,
		Output:      out,
		Command:     group.Command,
		Params:      params,
		UpdatedAt:   time.Now(),
	}
	if err := e.cache.Save(group.Name, entry); err != nil {
		e.log.Warn("cache save failed", "group", group.Name, "err", err.Error())
	}
}

// cachedOutput returns the last cached output for a group when available, so a
// skipped group can still satisfy downstream references. Returns "" otherwise.
func (e *Engine) cachedOutput(group *config.Group) string {
	if e.noCache || group.Cache == nil {
		return ""
	}
	if entry, ok := e.cache.Load(group.Name); ok {
		return entry.Output
	}
	return ""
}
