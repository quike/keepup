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
version: 2
groups:
  - name: echo
    command: echo
    params: ["hello"]
default: f
flows:
  f:
    description: "echo once"
    mode: step
    steps:
      - run: ["echo"]
`

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "keepup.yml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

func TestRootCmd_Help(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"--help"})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "Keepup runs YAML-declared groups")
}

func TestRunCmd_MissingConfig(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"run", "--config", "nonexistent.yml"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load configuration")
}

func TestRunCmd_UnknownFlow(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, minimalCfg)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"run", "ghost", "--config", cfgPath})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRunCmd_DefaultFlowUsedWhenNoArg(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, minimalCfg)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"run", "--config", cfgPath, "--dry-run"})
	require.NoError(t, cmd.Execute())
}

func TestRunCmd_NamedFlow(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, minimalCfg)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"run", "f", "--config", cfgPath, "--dry-run"})
	require.NoError(t, cmd.Execute())
}

func TestListCmd_Flows(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, minimalCfg)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"list", "--config", cfgPath})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "f ")
	assert.Contains(t, out.String(), "echo once")
}

func TestListCmd_Groups(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, minimalCfg)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"list", "groups", "--config", cfgPath})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "echo")
}

func TestListCmd_UnknownTarget(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, minimalCfg)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"list", "frobnicate", "--config", cfgPath})
	err := cmd.Execute()
	require.Error(t, err)
}

func TestValidateCmd(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, minimalCfg)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"validate", "--config", cfgPath})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "ok")
}

func TestVerboseDumpsConfig(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, minimalCfg)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"run", "--config", cfgPath, "--dry-run", "--verbose"})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "# config:")
}

func TestDefaultConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	got, err := defaultConfigPath()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".config", "keepup", "keepup.yml"), got)
}

func TestDefaultConfigPath_HomeMissingFails(t *testing.T) {
	if home := os.Getenv("HOME"); home == "" {
		t.Skip("HOME not set on this platform")
	}
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	_, err := defaultConfigPath()
	require.Error(t, err)
}

func TestLoad_DefaultsToHomeConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"validate"})
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
	cfg := &config.Config{Version: 2}
	err := dumpConfig(failingWriter{}, "/tmp/keepup.yml", cfg)
	require.Error(t, err)
}

func TestExecute_Help(t *testing.T) {
	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })
	os.Args = []string{"keepup", "--help"}
	code := Execute()
	assert.Equal(t, 0, code)
}

func TestExecute_BadFlagReturnsNonZero(t *testing.T) {
	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })
	os.Args = []string{"keepup", "run", "--config", "/no/such/path/keepup.yml"}
	code := Execute()
	assert.Equal(t, 1, code)
}
