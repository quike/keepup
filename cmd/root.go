// Package cmd implements the keepup CLI surface.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v4"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/configfile"
	"github.com/quike/keepup/internal/engine"
	"github.com/quike/keepup/internal/logger"
)

const (
	appName           = "keepup"
	listKindFlows     = "flows"
	listKindGroups    = "groups"
	noDescription     = "(no description)"
	validateCmdUse    = "validate"
	defaultFlowMarker = "(default)"
)

// runtimeOpts carries the parsed CLI state for one Execute() invocation.
// Centralizing state on a value (instead of package globals) lets tests run
// in parallel and makes the CLI wiring side-effect free.
type runtimeOpts struct {
	configFile string
	dryRun     bool
	verbose    bool
	noCache    bool
	// exitCode (--exit-code) makes a failed group's own exit status the
	// process exit status instead of 1.
	exitCode bool
	// dispatched records that cobra accepted the invocation and handed it to
	// a command; an error without it is a usage error.
	dispatched bool

	cfg *config.Config
	log logger.Logger
	// procGroups tracks running command process groups so a repeated
	// interrupt can force-kill them; nil outside Execute.
	procGroups *engine.ProcessGroups
}

// Execute runs the root command and returns the exit code. main() should call
// os.Exit on the returned value.
func Execute() int {
	groups := engine.NewProcessGroups()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	done := make(chan struct{})
	defer close(done)
	go interruptHandler{
		signals:   signals,
		cancel:    cancel,
		forceKill: groups.KillAll,
		exit:      os.Exit,
		stderr:    os.Stderr,
	}.run(done)

	return run(ctx, os.Args[1:], os.Stdout, os.Stderr, groups)
}

// run executes one CLI invocation and returns its exit code (see exitCode).
// groups (may be nil) tracks running commands for the force-kill on a
// repeated interrupt. It is the only place errors are reported: each is
// printed once to stderr, followed by the command's usage text when the
// invocation itself was wrong.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, groups *engine.ProcessGroups) int {
	opts := &runtimeOpts{procGroups: groups}
	root := buildRootCmd(opts, stdout, stderr)
	root.SetArgs(args)
	cmd, err := root.ExecuteContextC(ctx)
	if err != nil {
		if !opts.dispatched {
			err = &usageError{err: err}
		}
		reportError(stderr, cmd, err)
	}
	return exitCode(err, opts.exitCode)
}

// reportError prints err, plus cmd's usage text for a usage error.
func reportError(w io.Writer, cmd *cobra.Command, err error) {
	fmt.Fprintln(w, "Error:", err)
	var usage *usageError
	if errors.As(err, &usage) {
		fmt.Fprint(w, "\n"+cmd.UsageString())
	}
}

// newRootCmd builds a fresh root command tree wired to the given writers.
// It returns a brand-new tree on each call so tests can run safely in parallel.
func newRootCmd(stdout, stderr io.Writer) *cobra.Command {
	return buildRootCmd(&runtimeOpts{}, stdout, stderr)
}

// buildRootCmd builds the command tree around opts, which receives the parsed
// CLI state.
func buildRootCmd(opts *runtimeOpts, stdout, stderr io.Writer) *cobra.Command {
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
	// run() reports errors (and usage, for usage errors) exactly once; cobra's
	// own printing would duplicate them and send usage text to stdout.
	root.SilenceErrors = true
	root.SilenceUsage = true
	// Cobra runs this only after flags and arguments validated, so it marks
	// the boundary between usage errors and errors from the command itself.
	// No subcommand may define its own PersistentPreRun, or it would shadow
	// this one (guarded by TestPersistentPreRun_OnlyOnRoot).
	root.PersistentPreRun = func(*cobra.Command, []string) { opts.dispatched = true }

	configHelp := "Path to config file (default: nearest " + strings.Join(configfile.Names, ", ") +
		" in this or a parent directory, else ~/.config/" + appName + "/" + appName + ".yml)"
	root.PersistentFlags().StringVarP(&opts.configFile, "config", "c", "", configHelp)
	root.PersistentFlags().BoolVarP(&opts.dryRun, "dry-run", "d", false, "Dry run mode (no commands are executed)")
	root.PersistentFlags().BoolVarP(&opts.verbose, "verbose", "v", false, "Verbose output")

	root.AddCommand(newInitCmd(stdout))
	root.AddCommand(newRunCmd(opts))
	root.AddCommand(newWatchCmd(opts, stdout))
	root.AddCommand(newListCmd(opts, stdout))
	root.AddCommand(newValidateCmd(opts, stdout))
	root.AddCommand(newGraphCmd(opts, stdout))
	root.AddCommand(newMigrateCmd(stdout))
	root.AddCommand(newVersionCmd())
	return root
}

func newRunCmd(opts *runtimeOpts) *cobra.Command {
	var eventsPath string
	cmd := &cobra.Command{
		Use:   "run [flow]",
		Short: "Execute a flow (uses the configured default when no flow is given)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.load(cmd.ErrOrStderr()); err != nil {
				return err
			}
			var flowName string
			if len(args) == 1 {
				flowName = args[0]
			}
			var emitter engine.Emitter
			if eventsPath != "" {
				w, closeFn, err := openEventsWriter(eventsPath, cmd.OutOrStdout())
				if err != nil {
					return err
				}
				defer closeFn()
				emitter = engine.NewJSONEmitter(w)
			}
			e := engine.New(opts.cfg, opts.engineOptions(newChildOutput(cmd, eventsPath), emitter)...)
			return e.RunFlow(cmd.Context(), flowName)
		},
	}
	cmd.ValidArgsFunction = completeFlows(opts)
	cmd.Flags().BoolVar(&opts.noCache, "no-cache", false, "Ignore cached results; run every group")
	cmd.Flags().BoolVar(&opts.exitCode, "exit-code", false,
		"Exit with the failing command's own exit code instead of 1")
	cmd.Flags().StringVar(&eventsPath, "events", "", "Write a JSON event stream to this file ('-' for stdout)")
	return cmd
}

// eventsToStdout is the --events value that streams events to stdout.
const eventsToStdout = "-"

// engineOptions assembles the engine options run and watch share. A nil
// emitter keeps the engine's discard default. The engine builds its own runner;
// the CLI only says where that runner's live output goes.
func (o *runtimeOpts) engineOptions(out childOutput, emitter engine.Emitter) []engine.Option {
	engineOpts := []engine.Option{
		engine.WithLogger(o.log),
		engine.WithOutput(out.stdout, out.stderr),
		engine.WithDryRun(o.dryRun || o.cfg.Settings.DryRun),
		engine.WithNoCache(o.noCache),
		engine.WithProcessGroups(o.procGroups),
	}
	if emitter != nil {
		engineOpts = append(engineOpts, engine.WithEmitter(emitter))
	}
	return engineOpts
}

// childOutput is where the commands keepup runs write their live output.
type childOutput struct{ stdout, stderr io.Writer }

// newChildOutput wires child output to the cobra command's streams.
func newChildOutput(cmd *cobra.Command, eventsPath string) childOutput {
	return childOutput{
		stdout: childStdout(eventsPath, cmd.OutOrStdout(), cmd.ErrOrStderr()),
		stderr: cmd.ErrOrStderr(),
	}
}

// childStdout picks where a command's stdout goes. It is normally the CLI's
// stdout, so piping keepup pipes the command output; --events - claims stdout
// for the NDJSON stream, so child stdout joins stderr instead.
func childStdout(eventsPath string, stdout, stderr io.Writer) io.Writer {
	if eventsPath == eventsToStdout {
		return stderr
	}
	return stdout
}

// openEventsWriter resolves the --events target: "-" means stdout, otherwise a
// file (truncated). The returned closer is a no-op for stdout.
func openEventsWriter(path string, stdout io.Writer) (io.Writer, func(), error) {
	if path == eventsToStdout {
		return stdout, func() {}, nil
	}
	f, err := os.Create(filepath.Clean(path))
	if err != nil {
		return nil, nil, fmt.Errorf("open events file %q: %w", path, err)
	}
	return f, func() { _ = f.Close() }, nil
}

func newListCmd(opts *runtimeOpts, stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:       "list [flows|groups]",
		Short:     "List declared flows (default) or groups",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{listKindFlows, listKindGroups},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.load(cmd.ErrOrStderr()); err != nil {
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
				return &usageError{err: fmt.Errorf("unknown list target %q (expected %q or %q)",
					kind, listKindFlows, listKindGroups)}
			}
		},
	}
}

func newValidateCmd(opts *runtimeOpts, stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   validateCmdUse,
		Short: "Parse and validate the config file without running anything",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.load(cmd.ErrOrStderr()); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "%s: ok (%d groups, %d flows)\n",
				opts.configFile, len(opts.cfg.Groups), len(opts.cfg.Flows))
			return nil
		},
	}
}

// sortedFlowNames returns the declared flow names in a stable order, shared by
// `keepup list` and flow-name completion so the two cannot drift.
func sortedFlowNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Flows))
	for n := range cfg.Flows {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func printFlows(out io.Writer, cfg *config.Config) error {
	for _, n := range sortedFlowNames(cfg) {
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

// load locates and reads the config file and initializes the logger.
// Diagnostics (keepup's logs and the --verbose config dump) go to diag, the
// CLI's stderr, never to stdout: stdout is reserved for command output,
// list/validate/graph results, and --events -.
func (o *runtimeOpts) load(diag io.Writer) error {
	path, err := configfile.NewLocator().Locate(o.configFile)
	if err != nil {
		return &config.Error{Err: err}
	}
	o.configFile = path
	cfg, err := config.LoadConfig(path)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	o.cfg = cfg
	o.log = logger.NewWithWriter(diag, cfg.Settings.Logging.Level, cfg.Settings.Logging.Pretty)

	if o.verbose {
		if err := dumpConfig(diag, path, cfg); err != nil {
			return fmt.Errorf("dump config: %w", err)
		}
	}
	return nil
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
