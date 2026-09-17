package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraphCmd_FormatDot(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, flowsForGraph)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"graph", "dev", "--config", cfgPath, "--format", "dot"})
	require.NoError(t, cmd.Execute())

	got := out.String()
	assert.Contains(t, got, "digraph")
	assert.Contains(t, got, "->")
	assert.NotContains(t, got, "graph TD")
}

func TestGraphCmd_UnknownFormatIsRejected(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, flowsForGraph)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"graph", "dev", "--config", cfgPath, "--format", "svg"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "svg")
}

func TestGraphCmd_OutputToFile(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, flowsForGraph)
	dest := filepath.Join(t.TempDir(), "ci.mmd")
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"graph", "dev", "--config", cfgPath, "--output", dest})
	require.NoError(t, cmd.Execute())

	written, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Contains(t, string(written), "graph TD")
	assert.Empty(t, out.String(), "nothing goes to stdout when writing a file")
}

func TestGraphCmd_OutputDashIsStdout(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, flowsForGraph)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"graph", "dev", "--config", cfgPath, "--output", "-"})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "graph TD")
}

func TestGraphCmd_UnwritableOutputErrors(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, flowsForGraph)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"graph", "dev", "--config", cfgPath,
		"--output", filepath.Join(t.TempDir(), "no", "such", "dir", "g.mmd")})
	require.Error(t, cmd.Execute())
}

func TestGraphCmd_FormatCompletes(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"__complete", "graph", "--format", ""})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "mermaid")
	assert.Contains(t, out.String(), "dot")
}
