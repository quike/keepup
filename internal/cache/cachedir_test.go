package cache

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
)

// jsonFP fingerprints a group whose reads glob matches every .json file below
// root, so it would match the cache directory's own entries too.
func jsonFP(t *testing.T, root, cacheDir string) string {
	t.Helper()
	fp, err := Compute(Inputs{
		Spec:     &config.Cache{Method: config.CacheHash, Reads: []string{filepath.Join(root, "**", "*.json")}},
		Commands: []config.CommandSpec{{Command: "go"}},
		CacheDir: cacheDir,
	})
	require.NoError(t, err)
	return fp
}

func TestCompute_IgnoresCacheDirMatches(t *testing.T) {
	tests := []struct {
		name     string
		cacheDir func(root string) string
	}{
		{"absolute cache-dir", func(root string) string { return filepath.Join(root, ".keepup-cache") }},
		{"relative cache-dir", func(string) string { return ".keepup-cache" }},
		{"unclean cache-dir", func(string) string { return "./sub/../.keepup-cache/" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			writeFile(t, filepath.Join(root, "config.json"), "{}")
			cacheDir := tc.cacheDir(root)
			before := jsonFP(t, root, cacheDir)

			writeFile(t, filepath.Join(root, ".keepup-cache", "build.json"), `{"fingerprint":"x"}`)
			writeFile(t, filepath.Join(root, ".keepup-cache", "nested", "deep.json"), "{}")

			assert.Equal(t, before, jsonFP(t, root, cacheDir), "cache entries must not feed the fingerprint")
		})
	}
}

func TestCompute_CacheDirExclusionIsPathAware(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, ".keepup-cache")
	before := jsonFP(t, root, cacheDir)

	// Shares the cache dir's name as a prefix but is a real input.
	writeFile(t, filepath.Join(root, ".keepup-cache-inputs", "a.json"), "{}")

	assert.NotEqual(t, before, jsonFP(t, root, cacheDir))
}

func TestCompute_EmptyCacheDirExcludesNothing(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	before := jsonFP(t, root, "")

	writeFile(t, filepath.Join(root, "a.json"), "{}")

	assert.NotEqual(t, before, jsonFP(t, root, ""))
}

func TestCompute_IgnoresCacheDirMatchesOfRootRelativeGlobs(t *testing.T) {
	root := t.TempDir()
	t.Chdir(t.TempDir())
	writeFile(t, filepath.Join(root, "config.json"), "{}")
	fp := func() string {
		t.Helper()
		got, err := Compute(Inputs{
			Spec:     &config.Cache{Method: config.CacheHash, Reads: []string{"**/*.json"}},
			Root:     root,
			Commands: []config.CommandSpec{{Command: "go"}},
			CacheDir: filepath.Join(root, ".keepup-cache"),
		})
		require.NoError(t, err)
		return got
	}
	before := fp()

	writeFile(t, filepath.Join(root, ".keepup-cache", "build.json"), `{"fingerprint":"x"}`)

	assert.Equal(t, before, fp(), "cache entries must not feed the fingerprint")
}
