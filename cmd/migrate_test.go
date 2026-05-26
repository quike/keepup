package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const v1Doc = `
version: 1
settings:
  logging:
    level: info
groups:
  - name: a
    command: echo
    params: ["hi"]
execution:
  - group: ["a"]
`

func TestMigrateCmd_ToStdout(t *testing.T) {
	t.Parallel()
	src := writeTempConfig(t, v1Doc)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"migrate", src})
	require.NoError(t, cmd.Execute())

	s := out.String()
	assert.Contains(t, s, "version: 2")
	assert.Contains(t, s, "flows:")
	assert.Contains(t, s, "main:")
}

func TestMigrateCmd_ToFile(t *testing.T) {
	t.Parallel()
	src := writeTempConfig(t, v1Doc)
	dst := filepath.Join(t.TempDir(), "v2.yml")
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"migrate", src, "-o", dst})
	require.NoError(t, cmd.Execute())

	assert.Contains(t, out.String(), "wrote "+dst)
	written, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Contains(t, string(written), "version: 2")
}

func TestMigrateCmd_CustomFlow(t *testing.T) {
	t.Parallel()
	src := writeTempConfig(t, v1Doc)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"migrate", src, "--flow", "ci"})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "ci:")
	assert.Contains(t, out.String(), "default: ci")
}

func TestMigrateCmd_MissingFile(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"migrate", "/no/such/file.yml"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read")
}

func TestMigrateCmd_NotV1(t *testing.T) {
	t.Parallel()
	src := writeTempConfig(t, "version: 2\ngroups: []\n")
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"migrate", src})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a v1 document")
}
