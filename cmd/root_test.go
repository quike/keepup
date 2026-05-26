package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
)

const minimalCfg = `
version: 1
settings:
  logging:
    level: warn
groups:
  - name: echo
    command: echo
    params: ["hello"]
execution:
  - group: ["echo"]
`

func TestRootCmd_Help(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"--help"})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "Keepup is a task runner")
}

func TestRootCmd_MissingConfig(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"--config", "nonexistent.yml"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load configuration")
}

func TestRootCmd_GroupNotFound(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, minimalCfg)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"--config", cfgPath, "--group", "does-not-exist"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `group "does-not-exist" not found`)
}

func TestRootCmd_DryRunHonored(t *testing.T) {
	t.Parallel()
	// A command that would fail if actually executed; dry-run must skip it.
	cfg := `
groups:
  - name: g
    command: this-does-not-exist
execution:
  - group: ["g"]
`
	cfgPath := writeTempConfig(t, cfg)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"--config", cfgPath, "--dry-run"})
	require.NoError(t, cmd.Execute())
}

func TestRootCmd_VerboseDumpsConfig(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, minimalCfg)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"--config", cfgPath, "--dry-run", "--verbose"})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "# config:")
}

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "keepup.yml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

func TestDefaultConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	got, err := defaultConfigPath()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".config", "keepup", "keepup.yml"), got)
}

func TestRootCmd_RunsEngineSuccessfully(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, minimalCfg)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"--config", cfgPath})
	require.NoError(t, cmd.Execute())
}

func TestRootCmd_FilterByGroupNarrowsExecution(t *testing.T) {
	t.Parallel()
	// A config with two groups but we only want to run "alpha"; "bravo"'s
	// failing command must not execute.
	cfg := `
groups:
  - name: alpha
    command: echo
    params: ["alpha-ok"]
  - name: bravo
    command: this-must-not-run
execution:
  - group: ["alpha", "bravo"]
`
	cfgPath := writeTempConfig(t, cfg)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"--config", cfgPath, "--group", "alpha"})
	require.NoError(t, cmd.Execute())
}

func TestExecute_Help(t *testing.T) {
	// Execute() reads os.Args via cobra. We temporarily swap them to drive the
	// real entrypoint and assert the exit code path.
	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })

	os.Args = []string{"keepup", "--help"}
	code := Execute()
	assert.Equal(t, 0, code)
}

func TestExecute_BadFlagReturnsNonZero(t *testing.T) {
	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })

	os.Args = []string{"keepup", "--config", "/no/such/path/keepup.yml"}
	code := Execute()
	assert.Equal(t, 1, code)
}

func TestLoad_DefaultsToHomeConfigPath(t *testing.T) {
	// No --config flag → load() must call defaultConfigPath. Point HOME at a
	// temp dir that lacks the default file so we get a controlled error from
	// LoadConfig, but the default-path branch is exercised.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load configuration")
}

// failingWriter returns an error on every Write so we can exercise the
// dumpConfig error path.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, assert.AnError }

func TestDumpConfig_PropagatesWriteError(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Version: 1}
	err := dumpConfig(failingWriter{}, "/tmp/keepup.yml", cfg)
	require.Error(t, err)
}
