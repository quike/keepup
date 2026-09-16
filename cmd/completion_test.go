package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
)

// An undescribed flow, the default flow, and two names sharing a "c" prefix,
// so ordering, descriptions, and filtering are all observable.
const completionCfg = `
version: 2
groups:
  - name: echo
    command: echo
    params: ["hello"]
default: ci
flows:
  ci:
    description: "lint, test, build"
    mode: step
    steps:
      - run: ["echo"]
  build:
    mode: step
    steps:
      - run: ["echo"]
  clean:
    description: "remove artifacts"
    mode: step
    steps:
      - run: ["echo"]
`

func TestCompleteFlows(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, completionCfg)

	tests := []struct {
		name       string
		args       []string
		toComplete string
		want       []string
	}{
		{
			name: "all flows sorted, default marked, undescribed bare",
			want: []string{
				"build",
				"ci\t(default) lint, test, build",
				"clean\tremove artifacts",
			},
		},
		{
			name:       "prefix filters to matching flows",
			toComplete: "c",
			want: []string{
				"ci\t(default) lint, test, build",
				"clean\tremove artifacts",
			},
		},
		{
			name:       "exact prefix keeps only that flow",
			toComplete: "clea",
			want:       []string{"clean\tremove artifacts"},
		},
		{
			name:       "unmatched prefix yields nothing",
			toComplete: "zz",
			want:       nil,
		},
		{
			name:       "flow argument already supplied",
			args:       []string{"ci"},
			toComplete: "",
			want:       nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := &runtimeOpts{configFile: cfgPath}
			got, directive := completeFlows(opts)(&cobra.Command{}, tt.args, tt.toComplete)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
		})
	}
}

func TestCompleteFlows_UnusableConfigIsSilent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string // empty means "do not create the file at all"
	}{
		{name: "missing file"},
		{name: "malformed yaml", body: "version: 2\nflows: [oops\n"},
		{name: "valid yaml but invalid schema", body: "version: 99\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfgPath := filepath.Join(t.TempDir(), "keepup.yml")
			if tt.body != "" {
				require.NoError(t, os.WriteFile(cfgPath, []byte(tt.body), 0o600))
			}
			opts := &runtimeOpts{configFile: cfgPath}
			got, directive := completeFlows(opts)(&cobra.Command{}, nil, "")
			assert.Nil(t, got)
			assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
		})
	}
}

func TestCompleteFlows_FallsBackToDefaultConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".config", appName)
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, appName+".yml"), []byte(completionCfg), 0o600))

	got, directive := completeFlows(&runtimeOpts{})(&cobra.Command{}, nil, "b")
	assert.Equal(t, []string{"build"}, got)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}

func TestCompleteFlows_UnresolvableHomeIsSilent(t *testing.T) {
	if home := os.Getenv("HOME"); home == "" {
		t.Skip("HOME not set on this platform")
	}
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	got, directive := completeFlows(&runtimeOpts{})(&cobra.Command{}, nil, "")
	assert.Nil(t, got)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}

// A Tab press must not influence the run that follows it.
func TestCompleteFlows_LeavesOptsUntouched(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, completionCfg)
	opts := &runtimeOpts{configFile: cfgPath}
	_, _ = completeFlows(opts)(&cobra.Command{}, nil, "")
	assert.Nil(t, opts.cfg)
	assert.Nil(t, opts.log)
}

func TestSortedFlowNames(t *testing.T) {
	t.Parallel()
	cfg, err := config.LoadConfig(writeTempConfig(t, completionCfg))
	require.NoError(t, err)
	assert.Equal(t, []string{"build", "ci", "clean"}, sortedFlowNames(cfg))
}

// The tests below drive cobra's real completion entry point, the only way to
// prove the ValidArgsFunction wiring is reachable and not merely correct.

func TestCompletion_FlowArgWiredOnEveryFlowCommand(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, completionCfg)

	for _, name := range []string{"run", "watch", "graph"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			cmd := newRootCmd(&out, &out)
			cmd.SetArgs([]string{cobra.ShellCompRequestCmd, name, "--config", cfgPath, ""})
			require.NoError(t, cmd.Execute())
			assert.Contains(t, out.String(), "build\n")
			assert.Contains(t, out.String(), "ci\t(default) lint, test, build\n")
			assert.Contains(t, out.String(), "clean\tremove artifacts\n")
			assert.Contains(t, out.String(), ":4\n")
		})
	}
}

func TestCompletion_VerboseDoesNotLeakConfigDump(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, completionCfg)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{cobra.ShellCompRequestCmd, "run", "--config", cfgPath, "--verbose", ""})
	require.NoError(t, cmd.Execute())
	assert.NotContains(t, out.String(), "# config:")
	assert.Contains(t, out.String(), "ci\t(default) lint, test, build\n")
}

func TestCompletion_ListTargets(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{cobra.ShellCompRequestCmd, "list", ""})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), listKindFlows+"\n")
	assert.Contains(t, out.String(), listKindGroups+"\n")
	assert.Contains(t, out.String(), ":4\n")
}

// validate takes no positional argument.
func TestCompletion_ValidateOffersNoFlows(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, completionCfg)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{cobra.ShellCompRequestCmd, validateCmdUse, "--config", cfgPath, ""})
	require.NoError(t, cmd.Execute())
	assert.NotContains(t, out.String(), "ci\t")
}
