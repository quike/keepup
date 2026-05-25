package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"github.com/quike/keepup/internal/app"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/logger"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type executorLogger struct {
	*zerolog.Logger
}

func (l *executorLogger) Info() app.LogEvent  { return l.Logger.Info() }
func (l *executorLogger) Error() app.LogEvent { return l.Logger.Error() }
func (l *executorLogger) Trace() app.LogEvent { return l.Logger.Trace() }
func (l *executorLogger) Warn() app.LogEvent  { return l.Logger.Warn() }

const (
	appName = "keepup"
)

var (
	configFile string
	cfg        *config.Config
	dryRun     bool
	verbose    bool
	groupName  string
)

func init() {
	rootCmd.AddCommand(versionCmd)
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "", "Path to config file (default is ~/.config/"+appName+"/"+appName+".yml)")
	rootCmd.PersistentFlags().BoolVarP(&dryRun, "dry-run", "d", false, "Dry run mode (no changes applied)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Debug mode (verbose output)")
	rootCmd.Flags().StringVarP(&groupName, "group", "g", "", "Group name to run (overrides config file)")
}

var rootCmd = &cobra.Command{
	Use:   "keepup",
	Short: "Executes keepup commands",
	Long:  `Keepup is a task runner that executes tasks based on a configuration file.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if configFile == "" {
			configFile = defaultConfig()
		}
	},
	PreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		cfg, err = config.LoadConfig(configFile)
		if err != nil {
			logger.GetLogger().Error().Msgf("Error loading config: %v", err)
			return errors.New("unable to load configuration file")
		}
		logger.Log = logger.NewLogger(cfg.Settings.Logging.Level, cfg.Settings.Logging.Pretty)
		if verbose {
			showConfig(*cfg)
		}
		logger.GetLogger().Info().Msgf("Config file found at: %v", configFile)
		if err := validateGroupParam(cfg, groupName); err != nil {
			return err
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		executor := app.NewExecutor(*cfg, &executorLogger{logger.GetLogger()})
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			logger.GetLogger().Info().Msg("Shutting down...")
			executor.Cancel()
		}()
		if err := executor.Run(); err != nil {
			logger.GetLogger().Error().Msgf("Execution failed: %v", err)
		}
		signal.Stop(sigCh)
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func defaultConfig() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", appName, appName+".yml")
}

func showConfig(cfg config.Config) {
	pretty, err := yaml.Marshal(cfg)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(pretty))
}

func validateGroupParam(cfg *config.Config, groupName string) error {
	if groupName != "" {
		found := false
		for _, g := range cfg.Groups {
			if g.Name == groupName {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("group %q not found in config", groupName)
		}
		logger.GetLogger().Info().Msgf("Execution filtered by group: %v.", groupName)
		cfg.Execution = []config.Step{
			{Group: []string{groupName}},
		}
	}
	return nil
}
