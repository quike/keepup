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
	"github.com/quike/keepup/internal/result"
	"github.com/quike/keepup/internal/template"
)

// Engine binds a parsed Config to a set of pluggable collaborators.
type Engine struct {
	cfg            *config.Config
	groups         map[string]config.Group
	runner         Runner
	prober         Prober
	outputs        OutputStore
	expander       template.Expander
	cache          cache.Store
	emitter        Emitter
	log            logger.Logger
	maxConcurrency int
	dryRun         bool
	noCache        bool
	retryBackoff   time.Duration
}

// DefaultRetryBackoff is the base delay between retry attempts; the delay for
// attempt N is DefaultRetryBackoff * N.
const DefaultRetryBackoff = 250 * time.Millisecond

// Option configures an Engine.
type Option func(*Engine)

// WithRunner overrides the default ShellRunner.
func WithRunner(r Runner) Option { return func(e *Engine) { e.runner = r } }

// WithProber overrides the default ShellProber used for skip-if/require.
func WithProber(p Prober) Option { return func(e *Engine) { e.prober = p } }

// WithOutputStore overrides the default in-memory OutputStore.
func WithOutputStore(s OutputStore) Option { return func(e *Engine) { e.outputs = s } }

// WithExpander overrides the default template.Expander.
func WithExpander(x template.Expander) Option { return func(e *Engine) { e.expander = x } }

// WithCache overrides the default file-backed cache store.
func WithCache(s cache.Store) Option { return func(e *Engine) { e.cache = s } }

// WithNoCache disables cache reads/writes for this run.
func WithNoCache(disable bool) Option { return func(e *Engine) { e.noCache = disable } }

// WithLogger overrides the default no-op Logger.
func WithLogger(l logger.Logger) Option { return func(e *Engine) { e.log = l } }

// WithDryRun forces dry-run mode regardless of the config flag.
func WithDryRun(dry bool) Option { return func(e *Engine) { e.dryRun = dry } }

// WithEmitter sets the structured event emitter (default: discard).
func WithEmitter(em Emitter) Option { return func(e *Engine) { e.emitter = em } }

// WithRetryBackoff overrides the base retry backoff (delay for attempt N is
// base*N). Primarily useful in tests to avoid real sleeps.
func WithRetryBackoff(d time.Duration) Option { return func(e *Engine) { e.retryBackoff = d } }

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
		expander:       template.NewExpander(),
		cache:          cache.NewFileStore(cacheDir),
		emitter:        nopEmitter{},
		log:            logger.Nop(),
		maxConcurrency: cfg.Settings.MaxConcurrency,
		dryRun:         cfg.Settings.DryRun,
		retryBackoff:   DefaultRetryBackoff,
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
	flow := e.cfg.Flows[flowName]
	e.log.Info("starting flow", "flow", flowName, "mode", string(p.Mode))
	e.emitter.Emit(Event{Event: EventFlowStart, Flow: flowName, Mode: string(p.Mode)})

	start := time.Now()
	switch p.Mode {
	case config.ModeStep:
		err = e.runStepPlan(ctx, p, &flow)
	case config.ModeDAG:
		err = e.runDAGPlan(ctx, p, &flow)
	default:
		err = fmt.Errorf("flow %q: unknown mode %q", flowName, p.Mode)
	}
	status := StatusOK
	if err != nil {
		status = StatusFailed
	}
	e.emitter.Emit(Event{
		Event: EventFlowEnd, Flow: flowName, Status: status,
		DurationMS: msSince(start), Err: errString(err),
	})
	return err
}

// envelope is the resolved control envelope for a group's command execution.
type envelope struct {
	timeout time.Duration
	retries int
}

// resolveEnvelope computes the effective timeout/retries for a wave. A step's
// non-empty timeout / non-zero retries override the flow defaults; otherwise
// the flow values apply. step may be nil (dag mode).
func resolveEnvelope(flow *config.Flow, step *config.Step) envelope {
	timeout, retries := flow.Timeout, flow.Retries
	if step != nil {
		if step.Timeout != "" {
			timeout = step.Timeout
		}
		if step.Retries != 0 {
			retries = step.Retries
		}
	}
	// Durations are validated at config load, so a parse error here is
	// impossible; treat any residual error as "no timeout".
	d, _ := time.ParseDuration(timeout)
	return envelope{timeout: d, retries: retries}
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
// {{ output.X }} references resolve. The command run (only) is wrapped with the
// envelope's per-attempt timeout and bounded retries.
func (e *Engine) runGroup(ctx context.Context, group *config.Group, baseline map[string]result.RunResult, env envelope) (err error) {
	start := time.Now()
	e.emitter.Emit(Event{Event: EventGroupStart, Group: group.Name})
	status := StatusOK
	defer func() {
		if err != nil {
			status = StatusFailed
		}
		e.emitter.Emit(Event{
			Event: EventGroupEnd, Group: group.Name, Status: status,
			DurationMS: msSince(start), Err: errString(err),
		})
	}()

	data := template.Data{Outputs: baseline, Env: e.cfg.Env}

	// Expand the command and every param against the available outputs/env.
	// A copy of the group carries the expanded command so the runner, cache,
	// and logs all see the resolved value.
	g := *group
	cmd, err := e.expander.Expand(group.Command, data)
	if err != nil {
		return fmt.Errorf("group %q: expand command: %w", group.Name, err)
	}
	g.Command = cmd

	expanded := make([]string, len(group.Params))
	for i, p := range group.Params {
		expanded[i], err = e.expander.Expand(p, data)
		if err != nil {
			return fmt.Errorf("group %q: expand param %d: %w", group.Name, i+1, err)
		}
	}

	if e.dryRun {
		e.log.Info("[dry-run] would run",
			"group", g.Name, "command", g.Command, "params", expanded, "shell", g.UseShell())
		e.outputs.Set(g.Name, result.RunResult{Status: result.StatusDryRun})
		status = StatusDryRun
		return nil
	}

	if g.Require != "" {
		if err = e.prober.Probe(ctx, g.Require, e.cfg.Env); err != nil {
			return fmt.Errorf("group %q: requirement %q not met: %w", g.Name, g.Require, err)
		}
	}

	if g.SkipIf != "" {
		if err = e.prober.Probe(ctx, g.SkipIf, e.cfg.Env); err == nil {
			e.outputs.Set(g.Name, result.RunResult{Status: result.StatusSkipped})
			e.log.Info("group skipped", "group", g.Name, "reason", "skip-if", "predicate", g.SkipIf)
			status = StatusSkipped
			return nil
		}
	}

	if fp, hit := e.cacheLookup(&g, expanded); hit {
		cached := fp.Result
		cached.Status = result.StatusCached
		e.outputs.Set(g.Name, cached)
		e.log.Info("cache hit", "group", g.Name, "fingerprint", fp.Fingerprint)
		status = StatusCacheHit
		return nil
	}

	e.log.Info("running group", "group", g.Name, "command", g.Command, "params", expanded)
	out, err := e.execWithEnvelope(ctx, &g, expanded, env)
	if err != nil {
		e.log.Error("group failed", "group", g.Name, "err", err.Error(), "output", out.Output)
		return err
	}
	e.log.Trace("group output", "group", g.Name, "output", out.Output)
	// Runner sets Status to result.StatusOK on success; trust it so a future
	// soft-fail Runner can return Status:"failed" without engine clobbering.
	e.outputs.Set(g.Name, out)
	e.cacheStore(&g, expanded, &out)
	return nil
}

// execWithEnvelope runs the group's command, applying a per-attempt timeout and
// retrying up to env.retries additional times on failure. Backoff between
// attempts respects ctx cancellation.
func (e *Engine) execWithEnvelope(ctx context.Context, group *config.Group, params []string, env envelope) (result.RunResult, error) {
	attempts := 1 + env.retries
	var (
		out result.RunResult
		err error
	)
	for attempt := 1; attempt <= attempts; attempt++ {
		runCtx := ctx
		cancel := context.CancelFunc(func() {})
		if env.timeout > 0 {
			runCtx, cancel = context.WithTimeout(ctx, env.timeout)
		}
		out, err = e.runner.Run(runCtx, group, params, e.cfg.Env)
		cancel()
		if err == nil {
			return out, nil
		}
		if attempt < attempts {
			e.log.Warn("group attempt failed; retrying",
				"group", group.Name, "attempt", attempt, "of", attempts, "err", err.Error())
			select {
			case <-ctx.Done():
				return out, ctx.Err()
			case <-time.After(e.retryBackoff * time.Duration(attempt)):
			}
		}
	}
	return out, err
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
func (e *Engine) cacheStore(group *config.Group, params []string, out *result.RunResult) {
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
		Result:      *out,
		Command:     group.Command,
		Params:      params,
		UpdatedAt:   time.Now(),
	}
	if err := e.cache.Save(group.Name, entry); err != nil {
		e.log.Warn("cache save failed", "group", group.Name, "err", err.Error())
	}
}
