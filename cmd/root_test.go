package cmd

import (
	"bytes"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
)

func TestRootCmd_Execute(t *testing.T) {
	t.Cleanup(resetRootCmdState)
	var output bytes.Buffer
	rootCmd.SetOut(&output)
	rootCmd.SetArgs([]string{"--help"})

	_ = rootCmd.Execute()

	actualOutput := output.String()
	assert.Contains(t, actualOutput, "Keepup is a task runner that executes tasks based on")
}

func TestRootCmd_PreRunE(t *testing.T) {
	t.Cleanup(resetRootCmdState)
	var output bytes.Buffer
	rootCmd.SetOut(&output)
	rootCmd.SetArgs([]string{"--config", "nonexistent.yml"})

	err := rootCmd.Execute()
	assert.Error(t, err, "unable to load configuration file")
}

func TestRootCmd_ValidateGroupParam(t *testing.T) {
	t.Cleanup(resetRootCmdState)
	var output bytes.Buffer
	rootCmd.SetOut(&output)
	rootCmd.SetArgs([]string{"--group", "nonexistent-group"})

	err := rootCmd.Execute()
	assert.Error(t, err, "group 'nonexistent-group' not found in config")
}

// Cobra commands are mutable, and since rootCmd is a shared global, tests that mutate it
// (e.g., setting args, flags, or outputs) will affect other tests if you don’t reset it between tests
func resetRootCmdState() {
	rootCmd.SetArgs(nil)
	rootCmd.SetOut(nil)
	rootCmd.SetErr(nil)
	rootCmd.SilenceErrors = false
	rootCmd.SilenceUsage = false

	// Reset all flags
	rootCmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
	})
	rootCmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
	})
}
