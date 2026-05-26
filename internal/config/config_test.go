package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validYAML = `
version: 1

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
    description: "This runs program 1"
    command: "echo"
    params: ["hello"]

execution:
  - group: ["group1"]
`

func TestNewConfig(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		wantErr   bool
		assertion func(t *testing.T, cfg *Config)
	}{
		{
			name: "valid YAML",
			yaml: validYAML,
			assertion: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 1, cfg.Version)
				assert.Equal(t, "info", cfg.Settings.Logging.Level)
				assert.Equal(t, "/tmp", cfg.Settings.WorkingDir)
				assert.Equal(t, 2, cfg.Settings.MaxConcurrency)
				require.Len(t, cfg.Groups, 1)
				assert.Equal(t, "group1", cfg.Groups[0].Name)
				assert.Equal(t, "value", cfg.Env["KEY"])
			},
		},
		{
			name:    "invalid YAML",
			yaml:    `invalid_yaml: [`,
			wantErr: true,
		},
		{
			name: "empty YAML",
			yaml: "",
			assertion: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 0, cfg.Version)
				assert.Nil(t, cfg.Groups)
				assert.Nil(t, cfg.Execution)
				assert.Nil(t, cfg.Env)
			},
		},
		{
			name: "dry-run flag honored",
			yaml: "settings:\n  dry-run: true\n",
			assertion: func(t *testing.T, cfg *Config) {
				assert.True(t, cfg.Settings.DryRun)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := NewConfig([]byte(tc.yaml))
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.assertion != nil {
				tc.assertion(t, cfg)
			}
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
		assert.Equal(t, 1, cfg.Version)
	})

	t.Run("resource file", func(t *testing.T) {
		cfg, err := LoadConfig("./test-resources/config-valid.yml")
		require.NoError(t, err)
		assert.Equal(t, 1, cfg.Version)
		assert.Equal(t, "debug", cfg.Settings.Logging.Level)
		assert.Equal(t, "/tmp", cfg.Settings.WorkingDir)
		assert.Equal(t, 2, cfg.Settings.MaxConcurrency)
		require.Len(t, cfg.Groups, 3)
		assert.Equal(t, "global value", cfg.Env["WHATEVER"])
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

	t.Run("empty file", func(t *testing.T) {
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
		t.Setenv("USERPROFILE", home) // windows

		name := "keepup-test.yaml"
		require.NoError(t, os.WriteFile(filepath.Join(home, name), []byte(validYAML), 0o600))

		cfg, err := LoadConfig("~/" + name)
		require.NoError(t, err)
		assert.Equal(t, 1, cfg.Version)
	})

	t.Run("empty path is rejected", func(t *testing.T) {
		_, err := LoadConfig("")
		require.Error(t, err)
	})

	t.Run("short non-tilde path does not panic", func(t *testing.T) {
		_, err := LoadConfig("x")
		require.Error(t, err) // file not found, NOT a panic
	})
}

func TestLoadConfig_HomeExpansionWithMissingHomeFails(t *testing.T) {
	// Clear HOME/USERPROFILE so os.UserHomeDir cannot resolve a directory.
	// On Unix this makes UserHomeDir return an error, exercising the
	// expandHome error path.
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
		{"absolute path counts", "/usr/local/bin/zsh", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := Group{Shell: tc.shell}
			assert.Equal(t, tc.want, g.UseShell())
		})
	}
}
