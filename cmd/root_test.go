package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
