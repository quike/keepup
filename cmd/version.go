package cmd

import (
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Build-time variables, populated via -ldflags.
var (
	CLIVersion string
	CLIOs      string
	CLIArch    string
	CLISha     string
)

const versionUse = "version"

// versionEncode is the JSON encoder used by the version command. Exposed as a
// package-level variable so tests can substitute a failing encoder to exercise
// the otherwise-unreachable error branch (json.Marshal on a map[string]string
// cannot fail in practice).
var versionEncode = json.Marshal

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   versionUse,
		Short: "Display detailed version information",
		Long:  "Retrieve and display detailed version information about the app.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			arch := CLIArch
			if arch == "" {
				arch = runtime.GOARCH
			}
			osName := CLIOs
			if osName == "" {
				osName = runtime.GOOS
			}
			info := map[string]string{
				"version": CLIVersion,
				"os":      osName,
				"arch":    arch,
				"sha":     CLISha,
			}
			payload, err := versionEncode(info)
			if err != nil {
				return fmt.Errorf("encode version info: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(payload))
			return nil
		},
	}
}
