package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitCmd(t *testing.T) {
	t.Run("writes starter to given path", func(t *testing.T) {
		t.Parallel()
		dst := filepath.Join(t.TempDir(), "keepup.yml")
		var out bytes.Buffer
		cmd := newRootCmd(&out, &out)
		cmd.SetArgs([]string{"init", dst})
		require.NoError(t, cmd.Execute())

		assert.Contains(t, out.String(), "wrote "+dst)
		data, err := os.ReadFile(dst)
		require.NoError(t, err)
		assert.Contains(t, string(data), "version: 2")
		assert.Contains(t, string(data), "flows:")
	})

	t.Run("refuses to overwrite without --force", func(t *testing.T) {
		t.Parallel()
		dst := filepath.Join(t.TempDir(), "keepup.yml")
		require.NoError(t, os.WriteFile(dst, []byte("existing"), 0o600))
		var out bytes.Buffer
		cmd := newRootCmd(&out, &out)
		cmd.SetArgs([]string{"init", dst})
		err := cmd.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})

	t.Run("--force overwrites", func(t *testing.T) {
		t.Parallel()
		dst := filepath.Join(t.TempDir(), "keepup.yml")
		require.NoError(t, os.WriteFile(dst, []byte("existing"), 0o600))
		var out bytes.Buffer
		cmd := newRootCmd(&out, &out)
		cmd.SetArgs([]string{"init", dst, "--force"})
		require.NoError(t, cmd.Execute())
		data, _ := os.ReadFile(dst)
		assert.Contains(t, string(data), "version: 2")
	})

	t.Run("creates parent directories", func(t *testing.T) {
		t.Parallel()
		dst := filepath.Join(t.TempDir(), "nested", "dir", "keepup.yml")
		var out bytes.Buffer
		cmd := newRootCmd(&out, &out)
		cmd.SetArgs([]string{"init", dst})
		require.NoError(t, cmd.Execute())
		assert.FileExists(t, dst)
	})
}

func TestInitCmd_GeneratedConfigIsValid(t *testing.T) {
	t.Parallel()
	// The scaffold must always parse + validate, and running it must work.
	require.NoError(t, starterIsValid())

	dst := filepath.Join(t.TempDir(), "keepup.yml")
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"init", dst})
	require.NoError(t, cmd.Execute())

	val := newRootCmd(&out, &out)
	val.SetArgs([]string{"validate", "--config", dst})
	require.NoError(t, val.Execute())
}
