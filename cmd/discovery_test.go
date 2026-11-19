package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/configfile"
)

// isolateHome points HOME at an empty directory so the global fallback cannot
// pick up the developer's real config.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func execRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := newRootCmd(&out, &out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// The README quick start: `keepup init` then `keepup run`, here from a
// subdirectory of the project.
func TestQuickStart_InitThenRunFromSubdirectory(t *testing.T) {
	isolateHome(t)
	project := t.TempDir()
	t.Chdir(project)
	_, err := execRoot(t, "init")
	require.NoError(t, err)

	sub := filepath.Join(project, "pkg", "deep")
	require.NoError(t, os.MkdirAll(sub, 0o750))
	t.Chdir(sub)

	out, err := execRoot(t, "validate")
	require.NoError(t, err)
	assert.Contains(t, out, filepath.Join(filepath.Base(project), "keepup.yml")+": ok")

	_, err = execRoot(t, "run")
	require.NoError(t, err)
}

func TestLoad_NoConfigAnywhere(t *testing.T) {
	isolateHome(t)
	t.Chdir(t.TempDir())
	_, err := execRoot(t, "validate")
	require.ErrorIs(t, err, configfile.ErrNotFound)
}

func TestRun_NoConfigAnywhereIsAUsageError(t *testing.T) {
	isolateHome(t)
	t.Chdir(t.TempDir())
	var out bytes.Buffer
	assert.Equal(t, exitUsage, run(context.Background(), []string{"run"}, &out, &out, nil), out.String())
}

func TestLoad_ExplicitConfigBeatsDiscovery(t *testing.T) {
	isolateHome(t)
	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, "keepup.yml"), []byte("version: 99\n"), 0o600))
	t.Chdir(project)

	explicit := writeTempConfig(t, minimalCfg)
	out, err := execRoot(t, "validate", "-c", explicit)
	require.NoError(t, err)
	assert.Contains(t, out, explicit+": ok")
}

// settings.working-dir is the default directory for commands (#55) and, like
// every relative path in the config, resolves against the config's directory
// rather than the directory keepup was started from.
func TestRun_PathsResolveAgainstConfigDir(t *testing.T) {
	isolateHome(t)
	project := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(project, "out"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(project, "keepup.yml"), []byte(`
version: 2
settings:
  working-dir: out
groups:
  - name: mark
    command: touch
    params: ["marker"]
    cache:
      reads: ["out/marker"]
default: f
flows:
  f:
    steps:
      - run: [mark]
`), 0o600))
	elsewhere := filepath.Join(project, "elsewhere")
	require.NoError(t, os.MkdirAll(elsewhere, 0o750))
	t.Chdir(elsewhere)

	_, err := execRoot(t, "run", "-c", "../keepup.yml")
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(project, "out", "marker"))
	assert.DirExists(t, filepath.Join(project, ".keepup-cache"))
	assert.NoFileExists(t, filepath.Join(elsewhere, "marker"))
	assert.NoDirExists(t, filepath.Join(elsewhere, ".keepup-cache"))
}

func TestCompleteFlows_DiscoversProjectConfig(t *testing.T) {
	isolateHome(t)
	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, ".keepup.yml"), []byte(completionCfg), 0o600))
	sub := filepath.Join(project, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o750))
	t.Chdir(sub)

	got, directive := completeFlows(&runtimeOpts{})(&cobra.Command{}, nil, "b")
	assert.Equal(t, []string{"build"}, got)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}
