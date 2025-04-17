package cmd

import (
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/quike/keepup/logger"
	"github.com/spf13/cobra"
)

var (
	CLIVersion string
	CLIOs      string
	CLIArch    string
	CLISha     string
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Display detailed version information",
	Long:  `Retrieve and display detailed version information about the app.`,
	Run: func(cmd *cobra.Command, args []string) {
		if CLIArch == "" {
			CLIArch = runtime.GOARCH
		}
		versionInfo := map[string]string{
			"version": CLIVersion,
			"os":      CLIOs,
			"arch":    CLIArch,
			"sha":     CLISha,
		}
		versionJSON, err := json.Marshal(versionInfo)
		if err != nil {
			logger.GetLogger().Error().Msgf("Failed to encode version information: %v", err)
			fmt.Println("Error marshalling version info to JSON:", err)
			return
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(versionJSON))
	},
}
