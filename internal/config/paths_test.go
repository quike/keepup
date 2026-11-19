package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_RecordsConfigDir(t *testing.T) {
	t.Run("absolute path", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "keepup.yml")
		require.NoError(t, os.WriteFile(path, []byte(validYAML), 0o600))
		cfg, err := LoadConfig(path)
		require.NoError(t, err)
		assert.Equal(t, dir, cfg.Dir)
	})

	t.Run("relative path is made absolute", func(t *testing.T) {
		cfg, err := LoadConfig("./test-resources/config-valid.yml")
		require.NoError(t, err)
		want, err := filepath.Abs("test-resources")
		require.NoError(t, err)
		assert.Equal(t, want, cfg.Dir)
	})

	t.Run("bytes have no directory", func(t *testing.T) {
		cfg, err := NewConfig([]byte(validYAML))
		require.NoError(t, err)
		assert.Empty(t, cfg.Dir)
	})
}

func TestConfig_ResolvePath(t *testing.T) {
	root := filepath.FromSlash("/proj")
	abs := filepath.FromSlash("/elsewhere/x")
	cases := []struct {
		name, dir, in, want string
	}{
		{"relative joins the config dir", root, "build/out", filepath.Join(root, "build", "out")},
		{"parent traversal is cleaned", root, "../shared", filepath.FromSlash("/shared")},
		{"absolute is kept", root, abs, abs},
		{"empty stays empty", root, "", ""},
		{"no config dir keeps it relative", "", "build/out", "build/out"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{Dir: tc.dir}
			assert.Equal(t, tc.want, c.ResolvePath(tc.in))
		})
	}
}

func TestConfig_CommandDir(t *testing.T) {
	root := filepath.FromSlash("/proj")
	cases := []struct {
		name, workingDir, dir, want string
	}{
		{"defaults to the config dir", "", "", root},
		{"working-dir is the default", "app", "", filepath.Join(root, "app")},
		{"absolute working-dir", filepath.FromSlash("/srv"), "", filepath.FromSlash("/srv")},
		{"command dir resolves against the config dir", "app", "web", filepath.Join(root, "web")},
		{"absolute command dir wins", "app", filepath.FromSlash("/tmp"), filepath.FromSlash("/tmp")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{Dir: root, Settings: Settings{WorkingDir: tc.workingDir}}
			assert.Equal(t, tc.want, c.CommandDir(tc.dir))
		})
	}

	t.Run("no config dir and no working-dir inherits the process cwd", func(t *testing.T) {
		assert.Empty(t, (&Config{}).CommandDir(""))
	})
}

func TestConfig_CachePath(t *testing.T) {
	root := filepath.FromSlash("/proj")
	assert.Equal(t, filepath.Join(root, DefaultCacheDir), (&Config{Dir: root}).CachePath())
	assert.Equal(t, filepath.Join(root, "c"), (&Config{Dir: root, Settings: Settings{CacheDir: "c"}}).CachePath())
	assert.Equal(t, DefaultCacheDir, (&Config{}).CachePath())
}
