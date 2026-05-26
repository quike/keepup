package migrate

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
)

const v1Sample = `
version: 1
settings:
  logging:
    level: debug
    pretty: true
  working-dir: /tmp
  max-concurrency: 2
env:
  KEY: value
groups:
  - name: a
    description: "first"
    command: echo
    params: ["hello"]
  - name: b
    command: echo
    params: ["{{ output.a }}"]
    shell: /bin/sh
execution:
  - group: ["a"]
  - group: ["b"]
`

func TestMigrate_HappyPath(t *testing.T) {
	t.Parallel()
	out, err := Migrate([]byte(v1Sample), Options{})
	require.NoError(t, err)

	// The output must be a valid v2 document.
	cfg, err := config.NewConfig(out)
	require.NoError(t, err)
	assert.Equal(t, 2, cfg.Version)
	assert.Equal(t, DefaultFlowName, cfg.Default)

	// Groups preserved 1:1, including shell.
	require.Len(t, cfg.Groups, 2)
	assert.Equal(t, "echo", cfg.GroupByName("a").Command)
	assert.Equal(t, "/bin/sh", cfg.GroupByName("b").Shell)

	// Settings + env carried over.
	assert.Equal(t, "/tmp", cfg.Settings.WorkingDir)
	assert.Equal(t, 2, cfg.Settings.MaxConcurrency)
	assert.Equal(t, "debug", cfg.Settings.Logging.Level)
	assert.Equal(t, "value", cfg.Env["KEY"])

	// execution → a single step-mode flow with the same wave shape.
	flow := cfg.Flows[DefaultFlowName]
	assert.Equal(t, config.ModeStep, flow.Mode)
	require.Len(t, flow.Steps, 2)
	assert.Equal(t, []string{"a"}, flow.Steps[0].Run)
	assert.Equal(t, []string{"b"}, flow.Steps[1].Run)
}

func TestMigrate_CustomFlowName(t *testing.T) {
	t.Parallel()
	out, err := Migrate([]byte(v1Sample), Options{FlowName: "ci"})
	require.NoError(t, err)
	cfg, err := config.NewConfig(out)
	require.NoError(t, err)
	assert.Equal(t, "ci", cfg.Default)
	assert.Contains(t, cfg.Flows, "ci")
}

func TestMigrate_ParallelWavePreserved(t *testing.T) {
	t.Parallel()
	v1 := `
version: 1
groups:
  - { name: x, command: echo }
  - { name: y, command: echo }
execution:
  - group: ["x", "y"]
`
	out, err := Migrate([]byte(v1), Options{})
	require.NoError(t, err)
	cfg, err := config.NewConfig(out)
	require.NoError(t, err)
	assert.Equal(t, []string{"x", "y"}, cfg.Flows[DefaultFlowName].Steps[0].Run)
}

func TestMigrate_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"already v2", "version: 2\ngroups: []\n", "not a v1 document"},
		{"missing version", "groups: []\n", "not a v1 document"},
		{"no execution", "version: 1\ngroups:\n  - {name: a, command: echo}\n", "no 'execution' block"},
		{"bad yaml", "version: 1\nexecution: [", "parse v1 yaml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Migrate([]byte(tc.input), Options{})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestMigrate_RealV1Fixture(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../config/test-resources/config-valid-example-v1.yml")
	require.NoError(t, err)

	out, err := Migrate(data, Options{FlowName: "update"})
	require.NoError(t, err)

	cfg, err := config.NewConfig(out)
	require.NoError(t, err)
	assert.Equal(t, "update", cfg.Default)
	require.Len(t, cfg.Groups, 7) // brew-* + omf-* + fisher-update
	// The original 6-wave execution is preserved, incl. the parallel pair.
	flow := cfg.Flows["update"]
	require.Len(t, flow.Steps, 6)
	assert.Equal(t, []string{"omf-update", "fisher-update"}, flow.Steps[4].Run)
}

func TestMigrate_OutputHasNoEmptyNoise(t *testing.T) {
	t.Parallel()
	// A minimal v1 with no settings must not emit an empty settings block.
	v1 := "version: 1\ngroups:\n  - {name: a, command: echo}\nexecution:\n  - group: [a]\n"
	out, err := Migrate([]byte(v1), Options{})
	require.NoError(t, err)
	s := string(out)
	assert.NotContains(t, s, "settings:")
	assert.NotContains(t, s, "working-dir")
	assert.Contains(t, s, "version: 2")
}
