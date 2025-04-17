package cmd

import (
	"bytes"
	"encoding/json"
	"runtime"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestVersionCmd(t *testing.T) {
	CLIVersion = "1.0.0"
	CLIOs = runtime.GOOS
	CLIArch = runtime.GOARCH
	CLISha = "abc123"

	expectedVersionInfo := map[string]string{
		"version": CLIVersion,
		"os":      CLIOs,
		"arch":    CLIArch,
		"sha":     CLISha,
	}
	expectedJSON, _ := json.Marshal(expectedVersionInfo)

	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&output)

	versionCmd.Run(cmd, []string{})

	// Assertions
	actualOutput := output.String()
	assert.Equal(t, string(expectedJSON)+"\n", actualOutput)
}

func TestVersionCmdWithEmptyArch(t *testing.T) {
	CLIVersion = "1.0.0"
	CLIOs = runtime.GOOS
	CLIArch = ""
	CLISha = "abc123"

	expectedVersionInfo := map[string]string{
		"version": CLIVersion,
		"os":      CLIOs,
		"arch":    runtime.GOARCH, // Should default to runtime.GOARCH
		"sha":     CLISha,
	}
	expectedJSON, _ := json.Marshal(expectedVersionInfo)

	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&output)

	versionCmd.Run(cmd, []string{})

	// Assertions
	actualOutput := output.String()
	assert.Equal(t, string(expectedJSON)+"\n", actualOutput)
}
