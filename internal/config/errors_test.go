package config

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_FailuresAreTyped(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		path string
	}{
		{"empty path", ""},
		{"missing file", "test-resources/does-not-exist.yml"},
		{"invalid content", "test-resources/config-invalid.yml"},
		{"bad timeout", "test-resources/config-bad-timeout.yml"},
		{"bad reference", "test-resources/config-forward-ref.yml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := LoadConfig(tc.path)
			var ce *Error
			require.ErrorAs(t, err, &ce)
		})
	}
}

func TestNewConfig_UnparsableYAMLIsTyped(t *testing.T) {
	t.Parallel()
	_, err := NewConfig([]byte("groups: [unterminated"))
	var ce *Error
	assert.ErrorAs(t, err, &ce)
}

func TestError_PreservesMessageAndChain(t *testing.T) {
	t.Parallel()
	_, err := LoadConfig("test-resources/does-not-exist.yml")
	require.Error(t, err)
	assert.ErrorIs(t, err, fs.ErrNotExist, "the underlying cause stays reachable")
	assert.Contains(t, err.Error(), `read config file "test-resources/does-not-exist.yml"`)
}

func TestResolveFlow(t *testing.T) {
	t.Parallel()
	cfg, err := NewConfig([]byte(validYAML))
	require.NoError(t, err)

	t.Run("named flow", func(t *testing.T) {
		t.Parallel()
		name, err := cfg.ResolveFlow("ci")
		require.NoError(t, err)
		assert.Equal(t, "ci", name)
	})
	t.Run("empty name uses the default", func(t *testing.T) {
		t.Parallel()
		name, err := cfg.ResolveFlow("")
		require.NoError(t, err)
		assert.Equal(t, "ci", name)
	})
	t.Run("unknown flow", func(t *testing.T) {
		t.Parallel()
		_, err := cfg.ResolveFlow("ghost")
		var ce *Error
		require.ErrorAs(t, err, &ce)
		assert.EqualError(t, err, `flow "ghost" not found`)
	})
	t.Run("no name and no default", func(t *testing.T) {
		t.Parallel()
		noDefault := *cfg
		noDefault.Default = ""
		_, err := noDefault.ResolveFlow("")
		var ce *Error
		require.ErrorAs(t, err, &ce)
		assert.EqualError(t, err, "no flow specified and no default declared")
	})
}
