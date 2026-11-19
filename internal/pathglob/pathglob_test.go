package pathglob

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func touch(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, nil, 0o600))
}

// metaRoot returns a project root whose own path is full of glob
// metacharacters, so any code that splices the root into a pattern breaks.
func metaRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "[work]", "a*b?c", "app")
}

func TestGlob_RelativePatternsMatchUnderRoot(t *testing.T) {
	root := metaRoot(t)
	touch(t, filepath.Join(root, "src", "a.go"))
	touch(t, filepath.Join(root, "src", "deep", "b.go"))
	touch(t, filepath.Join(root, "go.mod"))
	touch(t, filepath.Join(filepath.Dir(root), "shared", "c.go"))

	cases := []struct {
		pattern string
		want    []string
	}{
		{"src/*.go", []string{"src/a.go"}},
		{"src/**/*.go", []string{"src/a.go", "src/deep/b.go"}},
		{"go.mod", []string{"go.mod"}},
		{"*.mod", []string{"go.mod"}},
		{"../shared/*.go", []string{"../shared/c.go"}},
		{"missing/*.go", nil},
	}
	for _, tc := range cases {
		t.Run(tc.pattern, func(t *testing.T) {
			got, err := Glob(root, tc.pattern)
			require.NoError(t, err)
			var want []string
			for _, w := range tc.want {
				want = append(want, filepath.FromSlash(w))
			}
			assert.ElementsMatch(t, want, got)
		})
	}
}

func TestGlob_AbsolutePatternIgnoresRoot(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "x.go"))
	got, err := Glob(metaRoot(t), filepath.Join(dir, "*.go"))
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(dir, "x.go")}, got)
}

func TestGlob_EmptyRootIsTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "x.go"))
	t.Chdir(dir)
	got, err := Glob("", "*.go")
	require.NoError(t, err)
	assert.Equal(t, []string{"x.go"}, got)
}

func TestGlob_BadPattern(t *testing.T) {
	_, err := Glob(t.TempDir(), "[invalid")
	require.Error(t, err)
}

func TestMatch(t *testing.T) {
	root := metaRoot(t)
	cases := []struct {
		name, pattern, path string
		want                bool
	}{
		{"relative pattern, path under root", "src/**/*.go", filepath.Join(root, "src", "x", "a.go"), true},
		{"relative pattern, other file", "src/**/*.go", filepath.Join(root, "README.md"), false},
		{"relative pattern, same name elsewhere", "src/*.go", filepath.Join(filepath.Dir(root), "src", "a.go"), false},
		{"parent-relative pattern", "../shared/*.go", filepath.Join(filepath.Dir(root), "shared", "c.go"), true},
		{"absolute pattern", "/abs/*.go", "/abs/a.go", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Match(root, filepath.FromSlash(tc.pattern), tc.path))
		})
	}
}
