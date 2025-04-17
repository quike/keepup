package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/quike/keepup/core/app"
	"github.com/quike/keepup/core/config"
	"github.com/quike/keepup/logger"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

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
		if err := validateGroupParam(cfg, groupName); err != nil {
			return err
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		executor := app.NewExecutor(*cfg)
		if err := executor.Run(); err != nil {
			logger.GetLogger().Error().Msgf("Execution failed: %v", err)
		}
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func defaultConfig() string {
	// Expand ~ to home dir
	home, err := os.UserHomeDir()
	if err != nil {
		logger.GetLogger().Error().Msgf("Cannot determine home dir: %v", err)
	}
	defaultConfig := filepath.Join(home, ".config", appName, appName+".yml")
	return defaultConfig
}

func showConfig(cfg config.Config) error {
	pretty, err := yaml.Marshal(cfg)
	if err != nil {
		panic(err)
	}
	logger.GetLogger().Info().Msgf("Config file found at: %v", configFile)
	fmt.Println(string(pretty))
	return nil
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
