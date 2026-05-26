// Package cmd implements the keepup CLI surface.
package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/engine"
	"github.com/quike/keepup/internal/logger"
)

const (
	appName        = "keepup"
	listKindFlows  = "flows"
	listKindGroups = "groups"
	noDescription  = "(no description)"
	validateCmdUse = "validate"
)

// runtimeOpts carries the parsed CLI state for one Execute() invocation.
// Centralizing state on a value (instead of package globals) lets tests run
// in parallel and makes the CLI wiring side-effect free.
type runtimeOpts struct {
	configFile string
	dryRun     bool
	verbose    bool
	noCache    bool

	cfg *config.Config
	log logger.Logger
}

// Execute runs the root command and returns the exit code. main() should call
// os.Exit on the returned value.
func Execute() int {
	root := newRootCmd(os.Stdout, os.Stderr)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// newRootCmd builds a fresh root command tree wired to the given writers.
// It returns a brand-new tree on each call so tests can run safely in parallel.
func newRootCmd(stdout, stderr io.Writer) *cobra.Command {
	opts := &runtimeOpts{}

	root := &cobra.Command{
		Use:   appName,
		Short: "Composable task runner with reusable groups and named flows",
		Long: "Keepup runs YAML-declared groups of commands inside named flows. " +
			"A flow is either a sequence of parallel waves (step mode) or a topologically " +
			"scheduled set of groups (dag mode); data flows between groups via " +
			"{{ output.<name> }} references.",
	}
	root.SetOut(stdout)
	root.SetErr(stderr)

	defaultHelp := "Path to config file (default is ~/.config/" + appName + "/" + appName + ".yml)"
	root.PersistentFlags().StringVarP(&opts.configFile, "config", "c", "", defaultHelp)
	root.PersistentFlags().BoolVarP(&opts.dryRun, "dry-run", "d", false, "Dry run mode (no commands are executed)")
	root.PersistentFlags().BoolVarP(&opts.verbose, "verbose", "v", false, "Verbose output")

	root.AddCommand(newRunCmd(opts))
	root.AddCommand(newWatchCmd(opts, stdout))
	root.AddCommand(newListCmd(opts, stdout))
	root.AddCommand(newValidateCmd(opts, stdout))
	root.AddCommand(newGraphCmd(opts, stdout))
	root.AddCommand(newVersionCmd())
	return root
}

func newRunCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run [flow]",
		Short: "Execute a flow (uses the configured default when no flow is given)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.load(cmd.OutOrStdout()); err != nil {
				return err
			}
			var flowName string
			if len(args) == 1 {
				flowName = args[0]
			}
			e := engine.New(opts.cfg,
				engine.WithLogger(opts.log),
				engine.WithDryRun(opts.dryRun || opts.cfg.Settings.DryRun),
				engine.WithNoCache(opts.noCache),
			)
			return e.RunFlow(cmd.Context(), flowName)
		},
	}
	cmd.Flags().BoolVar(&opts.noCache, "no-cache", false, "Ignore cached results; run every group")
	return cmd
}

func newListCmd(opts *runtimeOpts, stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "list [flows|groups]",
		Short: "List declared flows (default) or groups",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.load(cmd.OutOrStdout()); err != nil {
				return err
			}
			kind := listKindFlows
			if len(args) == 1 {
				kind = args[0]
			}
			switch kind {
			case listKindFlows:
				return printFlows(stdout, opts.cfg)
			case listKindGroups:
				return printGroups(stdout, opts.cfg)
			default:
				return fmt.Errorf("unknown list target %q (expected %q or %q)", kind, listKindFlows, listKindGroups)
			}
		},
	}
}

func newValidateCmd(opts *runtimeOpts, stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   validateCmdUse,
		Short: "Parse and validate the config file without running anything",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.load(cmd.OutOrStdout()); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "%s: ok (%d groups, %d flows)\n",
				opts.configFile, len(opts.cfg.Groups), len(opts.cfg.Flows))
			return nil
		},
	}
}

func printFlows(out io.Writer, cfg *config.Config) error {
	names := make([]string, 0, len(cfg.Flows))
	for n := range cfg.Flows {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		f := cfg.Flows[n]
		marker := " "
		if cfg.Default == n {
			marker = "*"
		}
		desc := f.Description
		if desc == "" {
			desc = noDescription
		}
		fmt.Fprintf(out, "%s %-20s [%s] %s\n", marker, n, f.Mode, desc)
	}
	return nil
}

func printGroups(out io.Writer, cfg *config.Config) error {
	for i := range cfg.Groups {
		g := &cfg.Groups[i]
		desc := g.Description
		if desc == "" {
			desc = noDescription
		}
		fmt.Fprintf(out, "  %-24s %s\n", g.Name, desc)
	}
	return nil
}

// load reads the config file and initializes the logger.
func (o *runtimeOpts) load(out io.Writer) error {
	path := o.configFile
	if path == "" {
		var err error
		path, err = defaultConfigPath()
		if err != nil {
			return err
		}
		o.configFile = path
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	o.cfg = cfg
	o.log = logger.New(cfg.Settings.Logging.Level, cfg.Settings.Logging.Pretty)

	if o.verbose {
		if err := dumpConfig(out, path, cfg); err != nil {
			return fmt.Errorf("dump config: %w", err)
		}
	}
	return nil
}

func defaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home dir: %w", err)
	}
	return filepath.Join(home, ".config", appName, appName+".yml"), nil
}

func dumpConfig(out io.Writer, path string, cfg *config.Config) error {
	pretty, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "# config: %s\n%s", path, pretty); err != nil {
		return err
	}
	return nil
}
