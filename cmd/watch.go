package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/engine"
	"github.com/quike/keepup/internal/watch"
)

func newWatchCmd(opts *runtimeOpts, _ io.Writer) *cobra.Command {
	var eventsPath string
	cmd := &cobra.Command{
		Use:   "watch [flow]",
		Short: "Re-run a flow whenever its groups' cache.reads inputs change",
		Long: "Watch the files declared in the cache.reads of the flow's groups and " +
			"re-run the flow on every change. Caching makes unaffected groups no-ops, " +
			"so only the work that actually depends on a changed file re-executes.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWatch(cmd, args, opts, eventsPath)
		},
	}
	cmd.Flags().StringVar(&eventsPath, "events", "",
		"Write a JSON event stream to this file ('-' for stdout)")
	return cmd
}

// resolveWatchFlow loads the config and returns the resolved flow name + flow.
func resolveWatchFlow(cmd *cobra.Command, args []string, opts *runtimeOpts) (string, config.Flow, error) {
	if err := opts.load(cmd.OutOrStdout()); err != nil {
		return "", config.Flow{}, err
	}
	flowName := opts.cfg.Default
	if len(args) == 1 {
		flowName = args[0]
	}
	if flowName == "" {
		return "", config.Flow{}, fmt.Errorf("no flow specified and no default declared")
	}
	flow, ok := opts.cfg.Flows[flowName]
	if !ok {
		return "", config.Flow{}, fmt.Errorf("flow %q not found", flowName)
	}
	return flowName, flow, nil
}

// watchSourceSetup bundles the fsnotify source, the dir count, and a cleanup
// closure so setupWatchSource has a self-documenting return type.
type watchSourceSetup struct {
	src      watch.Source
	dirCount int
	cleanup  func()
}

// setupWatchSource builds the fsnotify source and registers all resolved dirs.
func setupWatchSource(patterns []string) (watchSourceSetup, error) {
	setup := watchSourceSetup{cleanup: func() {}}
	src, err := watch.NewFSNotifySource()
	if err != nil {
		return setup, fmt.Errorf("create file watcher: %w", err)
	}
	dirs, err := watch.ResolveWatchDirs(patterns)
	if err != nil {
		_ = src.Close()
		return setup, fmt.Errorf("resolve watch dirs: %w", err)
	}
	for _, d := range dirs {
		if err := src.Add(d); err != nil {
			_ = src.Close()
			return setup, fmt.Errorf("watch %q: %w", d, err)
		}
	}
	setup.src = src
	setup.dirCount = len(dirs)
	setup.cleanup = func() { _ = src.Close() }
	return setup, nil
}

// runWatch is the body of the watch command, factored out of RunE so the
// cyclomatic complexity stays inside the project's gocyclo budget.
func runWatch(cmd *cobra.Command, args []string, opts *runtimeOpts, eventsPath string) error {
	flowName, flow, err := resolveWatchFlow(cmd, args, opts)
	if err != nil {
		return err
	}

	patterns := watchPatterns(opts.cfg, &flow)
	if len(patterns) == 0 {
		return fmt.Errorf(
			"flow %q has no watchable inputs: add a cache.reads block to at least one of its groups",
			flowName,
		)
	}

	setup, err := setupWatchSource(patterns)
	if err != nil {
		return err
	}
	defer setup.cleanup()

	// Banner goes to stderr so `--events -` can claim stdout for pure JSON.
	fmt.Fprintf(cmd.ErrOrStderr(),
		"watching %d dir(s) for flow %q; press Ctrl-C to stop\n",
		setup.dirCount, flowName)

	var emitter engine.Emitter
	if eventsPath != "" {
		ew, closeFn, oerr := openEventsWriter(eventsPath, cmd.OutOrStdout())
		if oerr != nil {
			return oerr
		}
		defer closeFn()
		emitter = engine.NewJSONEmitter(ew)
	}

	w := watch.New(patterns, setup.src, watch.WithLogger(opts.log))
	return w.Run(cmd.Context(), buildOnChange(emitter, opts, flowName))
}

// buildOnChange returns the per-tick callback the watcher invokes on each
// debounced batch. When an emitter is configured and the batch was triggered
// by file changes (len(files) > 0), it emits a watch.trigger event before the
// flow runs; the initial startup tick passes nil files and emits no trigger.
// Each tick runs the flow on a fresh engine sharing the one emitter so the
// event stream is a continuous sequence of per-tick flow envelopes.
func buildOnChange(emitter engine.Emitter, opts *runtimeOpts, flowName string) func(context.Context, []string) error {
	return func(ctx context.Context, files []string) error {
		if emitter != nil && len(files) > 0 {
			emitter.Emit(engine.Event{Event: engine.EventWatchTrigger, Flow: flowName, Files: files})
		}
		engineOpts := []engine.Option{
			engine.WithLogger(opts.log),
			engine.WithDryRun(opts.dryRun || opts.cfg.Settings.DryRun),
		}
		if emitter != nil {
			engineOpts = append(engineOpts, engine.WithEmitter(emitter))
		}
		e := engine.New(opts.cfg, engineOpts...)
		return e.RunFlow(ctx, flowName)
	}
}

// watchPatterns collects the de-duplicated cache.reads globs across every group
// in the flow. These are the inputs worth watching.
func watchPatterns(cfg *config.Config, flow *config.Flow) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, member := range flow.Members() {
		g := cfg.GroupByName(member)
		if g == nil || g.Cache == nil {
			continue
		}
		for _, r := range g.Cache.Reads {
			if _, dup := seen[r]; dup {
				continue
			}
			seen[r] = struct{}{}
			out = append(out, r)
		}
	}
	return out
}
