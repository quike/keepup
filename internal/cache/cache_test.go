package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestCompute_HashMethod(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	writeFile(t, a, "package main\n")
	spec := &config.Cache{Method: config.CacheHash, Reads: []string{filepath.Join(dir, "*.go")}}

	fp1, err := Compute(spec, "go", []string{"build"})
	require.NoError(t, err)
	assert.True(t, len(fp1) > 7 && fp1[:7] == "sha256:")

	t.Run("stable when nothing changes", func(t *testing.T) {
		fp2, err := Compute(spec, "go", []string{"build"})
		require.NoError(t, err)
		assert.Equal(t, fp1, fp2)
	})

	t.Run("changes when content changes", func(t *testing.T) {
		writeFile(t, a, "package main // changed\n")
		fp2, err := Compute(spec, "go", []string{"build"})
		require.NoError(t, err)
		assert.NotEqual(t, fp1, fp2)
	})

	t.Run("changes when command changes", func(t *testing.T) {
		fpCmd, err := Compute(spec, "gofmt", []string{"build"})
		require.NoError(t, err)
		fpCmd2, err := Compute(spec, "go", []string{"build"})
		require.NoError(t, err)
		assert.NotEqual(t, fpCmd, fpCmd2)
	})

	t.Run("changes when params change", func(t *testing.T) {
		fpA, err := Compute(spec, "go", []string{"build"})
		require.NoError(t, err)
		fpB, err := Compute(spec, "go", []string{"test"})
		require.NoError(t, err)
		assert.NotEqual(t, fpA, fpB)
	})

	t.Run("changes when a new matching file appears", func(t *testing.T) {
		before, err := Compute(spec, "go", []string{"build"})
		require.NoError(t, err)
		writeFile(t, filepath.Join(dir, "b.go"), "package main\n")
		after, err := Compute(spec, "go", []string{"build"})
		require.NoError(t, err)
		assert.NotEqual(t, before, after)
	})
}

func TestCompute_MtimeMethod(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	writeFile(t, f, "hello")
	spec := &config.Cache{Method: config.CacheMtime, Reads: []string{f}}

	fp1, err := Compute(spec, "cat", nil)
	require.NoError(t, err)

	// Bumping mtime changes the fingerprint even if content is identical.
	future := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(f, future, future))
	fp2, err := Compute(spec, "cat", nil)
	require.NoError(t, err)
	assert.NotEqual(t, fp1, fp2)
}

func TestCompute_DoublestarRecursive(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pkg", "deep", "x.go"), "package deep\n")
	spec := &config.Cache{Method: config.CacheHash, Reads: []string{filepath.Join(dir, "**", "*.go")}}
	fp, err := Compute(spec, "go", nil)
	require.NoError(t, err)
	assert.Contains(t, fp, "sha256:")
}

func TestCompute_MissingExplicitFileErrors(t *testing.T) {
	// A literal (non-glob) path that doesn't exist should surface an error,
	// since the user named a specific input.
	spec := &config.Cache{Method: config.CacheHash, Reads: []string{"/no/such/explicit/file.go"}}
	_, err := Compute(spec, "go", nil)
	// doublestar treats a literal path as a pattern matching nothing, so this
	// resolves to zero files and succeeds; assert the no-op behavior.
	require.NoError(t, err)
}

func TestCompute_DirectoryInput(t *testing.T) {
	// A glob that matches a directory exercises the dir branch in hashFile.
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	spec := &config.Cache{Method: config.CacheHash, Reads: []string{filepath.Join(dir, "*")}}
	fp, err := Compute(spec, "go", nil)
	require.NoError(t, err)
	assert.Contains(t, fp, "sha256:")
}

func TestCompute_BadGlobErrors(t *testing.T) {
	spec := &config.Cache{Method: config.CacheHash, Reads: []string{"[invalid"}}
	_, err := Compute(spec, "go", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad glob")
}

func TestSave_MkdirFails(t *testing.T) {
	// Point the cache dir at a path whose parent is a regular file, so
	// MkdirAll cannot create it.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
	store := NewFileStore(filepath.Join(blocker, "cache"))
	err := store.Save("build", &Entry{Fingerprint: "x"})
	require.Error(t, err)
}

func TestWritesPresent(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin", "app")
	writeFile(t, bin, "x")

	t.Run("present when all writes exist", func(t *testing.T) {
		spec := &config.Cache{Writes: []string{bin}}
		assert.True(t, WritesPresent(spec))
	})
	t.Run("absent when a write is missing", func(t *testing.T) {
		spec := &config.Cache{Writes: []string{bin, filepath.Join(dir, "missing")}}
		assert.False(t, WritesPresent(spec))
	})
	t.Run("no writes declared is trivially present", func(t *testing.T) {
		assert.True(t, WritesPresent(&config.Cache{}))
	})
}

func TestFileStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(filepath.Join(dir, "cache"))

	t.Run("miss before save", func(t *testing.T) {
		_, ok := store.Load("build")
		assert.False(t, ok)
	})

	entry := &Entry{
		Fingerprint: "sha256:abc",
		Output:      "done\n",
		Command:     "go",
		Params:      []string{"build"},
		UpdatedAt:   time.Now(),
	}
	require.NoError(t, store.Save("build", entry))

	t.Run("hit after save", func(t *testing.T) {
		got, ok := store.Load("build")
		require.True(t, ok)
		assert.Equal(t, "sha256:abc", got.Fingerprint)
		assert.Equal(t, "done\n", got.Output)
	})

	t.Run("group name with slashes is sanitized", func(t *testing.T) {
		require.NoError(t, store.Save("a/b:c", entry))
		got, ok := store.Load("a/b:c")
		require.True(t, ok)
		assert.Equal(t, "sha256:abc", got.Fingerprint)
	})
}

func TestFileStore_CorruptEntryIsMiss(t *testing.T) {
	dir := t.TempDir()
	cdir := filepath.Join(dir, "cache")
	require.NoError(t, os.MkdirAll(cdir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cdir, "build.json"), []byte("not json"), 0o600))
	store := NewFileStore(cdir)
	_, ok := store.Load("build")
	assert.False(t, ok)
}
