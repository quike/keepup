package migrate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
)

// migrateOK runs Migrate and parses the result, failing the test on any error.
func migrateOK(t *testing.T, v1 string) (cfg *config.Config, out string) {
	t.Helper()
	raw, err := Migrate([]byte(v1), Options{})
	require.NoError(t, err)
	cfg, err = config.NewConfig(raw)
	require.NoError(t, err)
	return cfg, string(raw)
}

func TestMigrate_SettingsVariants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		v1     string
		assert func(t *testing.T, cfg *config.Config, out string)
	}{
		{
			name: "logging only",
			v1: `version: 1
settings:
  logging:
    level: trace
groups: [{name: a, command: echo}]
execution: [{group: [a]}]`,
			assert: func(t *testing.T, cfg *config.Config, out string) {
				assert.Equal(t, "trace", cfg.Settings.Logging.Level)
				assert.NotContains(t, out, "working-dir")
				assert.NotContains(t, out, "max-concurrency")
			},
		},
		{
			name: "working-dir only",
			v1: `version: 1
settings:
  working-dir: /srv
groups: [{name: a, command: echo}]
execution: [{group: [a]}]`,
			assert: func(t *testing.T, cfg *config.Config, out string) {
				assert.Equal(t, "/srv", cfg.Settings.WorkingDir)
				assert.NotContains(t, out, "logging")
			},
		},
		{
			name: "max-concurrency only",
			v1: `version: 1
settings:
  max-concurrency: 8
groups: [{name: a, command: echo}]
execution: [{group: [a]}]`,
			assert: func(t *testing.T, cfg *config.Config, out string) {
				assert.Equal(t, 8, cfg.Settings.MaxConcurrency)
			},
		},
		{
			name: "pretty true is preserved",
			v1: `version: 1
settings:
  logging:
    level: info
    pretty: true
groups: [{name: a, command: echo}]
execution: [{group: [a]}]`,
			assert: func(t *testing.T, cfg *config.Config, out string) {
				assert.True(t, cfg.Settings.Logging.Pretty)
			},
		},
		{
			name: "no settings block emits none",
			v1: `version: 1
groups: [{name: a, command: echo}]
execution: [{group: [a]}]`,
			assert: func(t *testing.T, _ *config.Config, out string) {
				assert.NotContains(t, out, "settings:")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, out := migrateOK(t, tc.v1)
			tc.assert(t, cfg, out)
		})
	}
}

func TestMigrate_GroupFieldVariants(t *testing.T) {
	t.Parallel()

	t.Run("per-group env preserved", func(t *testing.T) {
		cfg, _ := migrateOK(t, `version: 1
groups:
  - name: a
    command: echo
    env:
      FOO: bar
execution: [{group: [a]}]`)
		assert.Equal(t, "bar", cfg.GroupByName("a").Env["FOO"])
	})

	t.Run("group without params omits params key", func(t *testing.T) {
		_, out := migrateOK(t, `version: 1
groups:
  - name: a
    command: echo
execution: [{group: [a]}]`)
		assert.NotContains(t, out, "params")
	})

	t.Run("shell path preserved", func(t *testing.T) {
		cfg, _ := migrateOK(t, `version: 1
groups:
  - name: a
    command: omf
    params: [update]
    shell: /opt/homebrew/bin/fish
execution: [{group: [a]}]`)
		assert.Equal(t, "/opt/homebrew/bin/fish", cfg.GroupByName("a").Shell)
	})

	t.Run("global env preserved", func(t *testing.T) {
		cfg, _ := migrateOK(t, `version: 1
env:
  GLOBAL: "1"
groups: [{name: a, command: echo}]
execution: [{group: [a]}]`)
		assert.Equal(t, "1", cfg.Env["GLOBAL"])
	})
}

func TestMigrate_ExecutionShapes(t *testing.T) {
	t.Parallel()

	t.Run("many sequential steps keep order", func(t *testing.T) {
		cfg, _ := migrateOK(t, `version: 1
groups:
  - {name: a, command: echo}
  - {name: b, command: echo}
  - {name: c, command: echo}
execution:
  - group: [a]
  - group: [b]
  - group: [c]`)
		flow := cfg.Flows[DefaultFlowName]
		require.Len(t, flow.Steps, 3)
		assert.Equal(t, []string{"a"}, flow.Steps[0].Run)
		assert.Equal(t, []string{"b"}, flow.Steps[1].Run)
		assert.Equal(t, []string{"c"}, flow.Steps[2].Run)
	})

	t.Run("valid cross-step output reference migrates", func(t *testing.T) {
		cfg, _ := migrateOK(t, `version: 1
groups:
  - {name: producer, command: echo, params: ["x"]}
  - name: consumer
    command: echo
    params: ["{{ output.producer }}"]
execution:
  - group: [producer]
  - group: [consumer]`)
		assert.Len(t, cfg.Flows[DefaultFlowName].Steps, 2)
	})
}

// TestMigrate_SurfacesLatentV1Bugs documents the most important class of edge
// case: v1 never validated references or group definitions, so a file that
// "worked" (or silently misbehaved) under v1 may be genuinely invalid. The
// migrator validates its output against the v2 parser and refuses to emit a
// broken file, surfacing the latent bug instead.
func TestMigrate_SurfacesLatentV1Bugs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		v1      string
		wantErr string
	}{
		{
			name: "same-step output reference (a race in v1) is rejected",
			v1: `version: 1
groups:
  - {name: a, command: echo, params: ["{{ output.b }}"]}
  - {name: b, command: echo}
execution:
  - group: [a, b]`,
			wantErr: "failed v2 validation",
		},
		{
			name: "reference to a group not in the flow",
			v1: `version: 1
groups:
  - {name: a, command: echo, params: ["{{ output.ghost }}"]}
execution:
  - group: [a]`,
			wantErr: "failed v2 validation",
		},
		{
			name: "execution references an undefined group",
			v1: `version: 1
groups:
  - {name: a, command: echo}
execution:
  - group: [a, missing]`,
			wantErr: "failed v2 validation",
		},
		{
			name: "duplicate group name",
			v1: `version: 1
groups:
  - {name: a, command: echo}
  - {name: a, command: ls}
execution:
  - group: [a]`,
			wantErr: "failed v2 validation",
		},
		{
			name: "group missing command",
			v1: `version: 1
groups:
  - {name: a}
execution:
  - group: [a]`,
			wantErr: "failed v2 validation",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Migrate([]byte(tc.v1), Options{})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestMigrate_VersionGuards(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		v1   string
	}{
		{"version 0 / missing", "groups: []\nexecution: []\n"},
		{"version 2", "version: 2\ngroups: []\nexecution: []\n"},
		{"version 3", "version: 3\ngroups: []\nexecution: []\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Migrate([]byte(tc.v1), Options{})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "not a v1 document")
		})
	}
}

func TestMigrate_OutputIsStableAndParseable(t *testing.T) {
	t.Parallel()
	// Migrating twice (v1 → v2, then feed v2 back through v1 parse) must reject
	// the v2 output as "not v1", proving the output is unambiguously v2.
	out, err := Migrate([]byte(v1Sample), Options{})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(strings.TrimSpace(string(out)), "version: 2"))

	_, err = Migrate(out, Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a v1 document")
}
