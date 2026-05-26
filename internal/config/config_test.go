package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validYAML = `
version: 2

settings:
  logging:
    level: info
    pretty: true
  working-dir: /tmp
  max-concurrency: 2

env:
  KEY: "value"

groups:
  - name: group1
    description: "produces a value"
    command: echo
    params: ["hello"]
  - name: group2
    description: "consumes group1"
    command: echo
    params: ["{{ output.group1 }}"]

default: ci

flows:
  ci:
    description: "build then test"
    mode: step
    steps:
      - run: ["group1"]
      - run: ["group2"]
`

func TestNewConfig_Valid(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		assertion func(t *testing.T, cfg *Config)
	}{
		{
			name: "valid v2 with step flow",
			yaml: validYAML,
			assertion: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 2, cfg.Version)
				assert.Equal(t, "info", cfg.Settings.Logging.Level)
				assert.Equal(t, "/tmp", cfg.Settings.WorkingDir)
				assert.Equal(t, 2, cfg.Settings.MaxConcurrency)
				require.Len(t, cfg.Groups, 2)
				require.Contains(t, cfg.Flows, "ci")
				assert.Equal(t, ModeStep, cfg.Flows["ci"].Mode)
				assert.Equal(t, "ci", cfg.Default)
				assert.Equal(t, "value", cfg.Env["KEY"])
			},
		},
		{
			name: "wholly empty document is accepted",
			yaml: "",
			assertion: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 0, cfg.Version)
				assert.Nil(t, cfg.Groups)
				assert.Nil(t, cfg.Flows)
			},
		},
		{
			name: "dry-run flag honored",
			yaml: "version: 2\nsettings:\n  dry-run: true\ngroups:\n  - name: x\n    command: echo\nflows:\n  f:\n    steps:\n      - run: [x]\n",
			assertion: func(t *testing.T, cfg *Config) {
				assert.True(t, cfg.Settings.DryRun)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := NewConfig([]byte(tc.yaml))
			require.NoError(t, err)
			tc.assertion(t, cfg)
		})
	}
}

func TestNewConfig_Errors(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name:    "v1 schema rejected",
			yaml:    "version: 1\ngroups:\n  - name: x\n    command: echo\n",
			wantErr: "unsupported schema version 1",
		},
		{
			name:    "missing version with content rejected",
			yaml:    "groups:\n  - name: x\n    command: echo\n",
			wantErr: "unsupported schema version 0",
		},
		{
			name:    "invalid YAML",
			yaml:    "invalid_yaml: [",
			wantErr: "unmarshal configuration",
		},
		{
			name: "duplicate group name",
			yaml: `version: 2
groups:
  - name: x
    command: echo
  - name: x
    command: ls
flows:
  f:
    steps:
      - run: [x]
`,
			wantErr: "duplicate name",
		},
		{
			name:    "missing flows",
			yaml:    "version: 2\ngroups:\n  - name: x\n    command: echo\n",
			wantErr: "at least one flow must be defined",
		},
		{
			name:    "flow references undefined group",
			yaml:    "version: 2\ngroups:\n  - name: x\n    command: echo\nflows:\n  f:\n    steps:\n      - run: [missing]\n",
			wantErr: "is not defined",
		},
		{
			name:    "step mode rejects 'run:' top-level",
			yaml:    "version: 2\ngroups:\n  - name: x\n    command: echo\nflows:\n  f:\n    mode: step\n    run: [x]\n",
			wantErr: "mode 'step' uses 'steps:'",
		},
		{
			name:    "dag mode rejects 'steps:'",
			yaml:    "version: 2\ngroups:\n  - name: x\n    command: echo\nflows:\n  f:\n    mode: dag\n    steps:\n      - run: [x]\n",
			wantErr: "mode 'dag' uses 'run:'",
		},
		{
			name:    "default must reference an existing flow",
			yaml:    "version: 2\ngroups:\n  - name: x\n    command: echo\ndefault: ghost\nflows:\n  f:\n    steps:\n      - run: [x]\n",
			wantErr: "not a declared flow",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewConfig([]byte(tc.yaml))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestLoadConfig(t *testing.T) {
	t.Run("valid file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(validYAML), 0o600))
		cfg, err := LoadConfig(path)
		require.NoError(t, err)
		assert.Equal(t, 2, cfg.Version)
	})

	t.Run("resource file with two flows", func(t *testing.T) {
		cfg, err := LoadConfig("./test-resources/config-valid.yml")
		require.NoError(t, err)
		assert.Equal(t, 2, cfg.Version)
		require.Len(t, cfg.Groups, 3)
		require.Contains(t, cfg.Flows, "pipeline")
		require.Contains(t, cfg.Flows, "pipeline-dag")
		assert.Equal(t, ModeStep, cfg.Flows["pipeline"].Mode)
		assert.Equal(t, ModeDAG, cfg.Flows["pipeline-dag"].Mode)
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := LoadConfig("nonexistent.yaml")
		require.Error(t, err)
	})

	t.Run("invalid YAML on disk", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.yaml")
		require.NoError(t, os.WriteFile(path, []byte("invalid_yaml: ["), 0o600))
		_, err := LoadConfig(path)
		require.Error(t, err)
	})

	t.Run("invalid resource file", func(t *testing.T) {
		_, err := LoadConfig("./test-resources/config-invalid.yml")
		require.Error(t, err)
	})

	t.Run("empty file is accepted", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "empty.yaml")
		require.NoError(t, os.WriteFile(path, nil, 0o600))
		cfg, err := LoadConfig(path)
		require.NoError(t, err)
		assert.Equal(t, 0, cfg.Version)
	})

	t.Run("expands ~/ to home", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		name := "keepup-test.yaml"
		require.NoError(t, os.WriteFile(filepath.Join(home, name), []byte(validYAML), 0o600))
		cfg, err := LoadConfig("~/" + name)
		require.NoError(t, err)
		assert.Equal(t, 2, cfg.Version)
	})

	t.Run("empty path is rejected", func(t *testing.T) {
		_, err := LoadConfig("")
		require.Error(t, err)
	})

	t.Run("short non-tilde path does not panic", func(t *testing.T) {
		_, err := LoadConfig("x")
		require.Error(t, err)
	})
}

func TestLoadConfig_HomeExpansionWithMissingHomeFails(t *testing.T) {
	if home := os.Getenv("HOME"); home == "" {
		t.Skip("HOME not normally set on this platform")
	}
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	_, err := LoadConfig("~/whatever.yaml")
	require.Error(t, err)
}

func TestGroup_UseShell(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		shell string
		want  bool
	}{
		{"empty shell field means direct exec", "", false},
		{"any non-empty value opts into shell mode", "/bin/sh", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := Group{Shell: tc.shell}
			assert.Equal(t, tc.want, g.UseShell())
		})
	}
}

func TestReferenceValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{
			name:    "DAG cycle is rejected",
			path:    "./test-resources/config-cycle.yml",
			wantErr: "cycle",
		},
		{
			name:    "same-step reference is rejected",
			path:    "./test-resources/config-forward-ref.yml",
			wantErr: "not produced by an earlier step",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadConfig(tc.path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestExtractRefs(t *testing.T) {
	t.Parallel()
	g := &Group{
		Command: "echo",
		Params: []string{
			"{{ output.a }} and {{ output.b }}",
			"static",
			"{{output.c}}",
			"{{   output.d   }}",
		},
	}
	got := ExtractRefs(g)
	assert.Equal(t, []string{"a", "b", "c", "d"}, got)
}

func TestMembers(t *testing.T) {
	t.Parallel()
	t.Run("step mode flattens steps", func(t *testing.T) {
		f := Flow{Mode: ModeStep, Steps: []Step{{Run: []string{"a", "b"}}, {Run: []string{"c"}}}}
		assert.Equal(t, []string{"a", "b", "c"}, f.Members())
	})
	t.Run("dag mode returns run set", func(t *testing.T) {
		f := Flow{Mode: ModeDAG, Run: []string{"a", "b"}}
		assert.Equal(t, []string{"a", "b"}, f.Members())
	})
}

func TestGroupByName(t *testing.T) {
	t.Parallel()
	cfg := &Config{Groups: []Group{{Name: "a"}, {Name: "b"}}}
	assert.NotNil(t, cfg.GroupByName("a"))
	assert.Nil(t, cfg.GroupByName("missing"))
}
