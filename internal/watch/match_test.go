package watch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatches(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		patterns []string
		path     string
		want     bool
	}{
		{"exact file", []string{"go.mod"}, "go.mod", true},
		{"single-level glob", []string{"*.go"}, "main.go", true},
		{"single-level glob no match in subdir", []string{"*.go"}, "sub/main.go", false},
		{"doublestar recursive", []string{"src/**/*.go"}, "src/a/b/c.go", true},
		{"doublestar at root", []string{"**/*.go"}, "deep/nested/x.go", true},
		{"no match", []string{"*.go"}, "README.md", false},
		{"one of several", []string{"*.md", "*.go"}, "main.go", true},
		{"dot-cleaned path", []string{"src/*.go"}, "./src/a.go", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Matches(tc.patterns, tc.path))
		})
	}
}

func TestGlobBase(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"src/**/*.go": "src",
		"*.go":        ".",
		"go.mod":      ".",
		"a/b/c.txt":   "a/b",
		"a/b/*.txt":   "a/b",
		"**/*.go":     ".",
	}
	for pattern, want := range tests {
		assert.Equal(t, want, globBase(pattern), "pattern=%q", pattern)
	}
}

func TestResolveWatchDirs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Build: root/src/a, root/src/a/deep, root/other
	mk := func(parts ...string) {
		p := filepath.Join(append([]string{root}, parts...)...)
		require.NoError(t, os.MkdirAll(p, 0o755))
	}
	mk("src", "a", "deep")
	mk("other")

	t.Run("recursive walk of glob base", func(t *testing.T) {
		dirs, err := ResolveWatchDirs([]string{filepath.Join(root, "src", "**", "*.go")})
		require.NoError(t, err)
		assert.Contains(t, dirs, filepath.Join(root, "src"))
		assert.Contains(t, dirs, filepath.Join(root, "src", "a"))
		assert.Contains(t, dirs, filepath.Join(root, "src", "a", "deep"))
		assert.NotContains(t, dirs, filepath.Join(root, "other"))
	})

	t.Run("missing base is skipped, not an error", func(t *testing.T) {
		dirs, err := ResolveWatchDirs([]string{filepath.Join(root, "does-not-exist", "*.go")})
		require.NoError(t, err)
		assert.Empty(t, dirs)
	})

	t.Run("literal file watches its directory", func(t *testing.T) {
		dirs, err := ResolveWatchDirs([]string{filepath.Join(root, "src", "go.mod")})
		require.NoError(t, err)
		assert.Contains(t, dirs, filepath.Join(root, "src"))
	})
}
