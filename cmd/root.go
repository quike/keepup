// Package cmd implements the keepup CLI surface.
package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/engine"
	"github.com/quike/keepup/internal/logger"
)

const appName = "keepup"

// runtimeOpts holds the parsed CLI flags for a single Execute() invocation.
// Centralizing state on a value (instead of package globals) lets tests run
// in parallel and makes the CLI wiring side-effect free.
type runtimeOpts struct {
	configFile string
	dryRun     bool
	verbose    bool
	groupName  string

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
		Short: "Executes keepup commands",
		Long:  "Keepup is a task runner that executes tasks based on a configuration file.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.load(stdout); err != nil {
				return err
			}
			if err := opts.filterByGroup(); err != nil {
				return err
			}
			e := engine.New(opts.cfg,
				engine.WithLogger(opts.log),
				engine.WithDryRun(opts.dryRun || opts.cfg.Settings.DryRun),
			)
			return e.Run(cmd.Context())
		},
	}
	root.SetOut(stdout)
	root.SetErr(stderr)

	defaultHelp := "Path to config file (default is ~/.config/" + appName + "/" + appName + ".yml)"
	root.PersistentFlags().StringVarP(&opts.configFile, "config", "c", "", defaultHelp)
	root.PersistentFlags().BoolVarP(&opts.dryRun, "dry-run", "d", false, "Dry run mode (no commands are executed)")
	root.PersistentFlags().BoolVarP(&opts.verbose, "verbose", "v", false, "Verbose output")
	root.Flags().StringVarP(&opts.groupName, "group", "g", "", "Group name to run (overrides config execution)")

	root.AddCommand(newVersionCmd())
	return root
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

// filterByGroup narrows the execution plan to a single group when --group is set.
func (o *runtimeOpts) filterByGroup() error {
	if o.groupName == "" {
		return nil
	}
	found := false
	for _, g := range o.cfg.Groups {
		if g.Name == o.groupName {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("group %q not found in config", o.groupName)
	}
	o.log.Info("filtering execution by group", "group", o.groupName)
	o.cfg.Execution = []config.Step{{Group: []string{o.groupName}}}
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
