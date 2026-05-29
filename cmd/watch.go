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

func newWatchCmd(opts *runtimeOpts, stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "watch [flow]",
		Short: "Re-run a flow whenever its groups' cache.reads inputs change",
		Long: "Watch the files declared in the cache.reads of the flow's groups and " +
			"re-run the flow on every change. Caching makes unaffected groups no-ops, " +
			"so only the work that actually depends on a changed file re-executes.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.load(cmd.OutOrStdout()); err != nil {
				return err
			}
			flowName := opts.cfg.Default
			if len(args) == 1 {
				flowName = args[0]
			}
			if flowName == "" {
				return fmt.Errorf("no flow specified and no default declared")
			}
			flow, ok := opts.cfg.Flows[flowName]
			if !ok {
				return fmt.Errorf("flow %q not found", flowName)
			}

			patterns := watchPatterns(opts.cfg, &flow)
			if len(patterns) == 0 {
				return fmt.Errorf(
					"flow %q has no watchable inputs: add a cache.reads block to at least one of its groups",
					flowName,
				)
			}

			src, err := watch.NewFSNotifySource()
			if err != nil {
				return fmt.Errorf("create file watcher: %w", err)
			}
			defer func() { _ = src.Close() }()

			dirs, err := watch.ResolveWatchDirs(patterns)
			if err != nil {
				return fmt.Errorf("resolve watch dirs: %w", err)
			}
			for _, d := range dirs {
				if err := src.Add(d); err != nil {
					return fmt.Errorf("watch %q: %w", d, err)
				}
			}

			fmt.Fprintf(stdout, "watching %d dir(s) for flow %q; press Ctrl-C to stop\n", len(dirs), flowName)

			w := watch.New(patterns, src, watch.WithLogger(opts.log))
			return w.Run(cmd.Context(), func(ctx context.Context, _ []string) error {
				e := engine.New(opts.cfg,
					engine.WithLogger(opts.log),
					engine.WithDryRun(opts.dryRun || opts.cfg.Settings.DryRun),
				)
				return e.RunFlow(ctx, flowName)
			})
		},
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
