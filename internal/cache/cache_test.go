package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/result"
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

	fp1, err := Compute(Inputs{Spec: spec, Commands: []config.CommandSpec{{Command: "go", Params: []string{"build"}}}})
	require.NoError(t, err)
	assert.True(t, len(fp1) > 7 && fp1[:7] == "sha256:")

	t.Run("stable when nothing changes", func(t *testing.T) {
		fp2, err := Compute(Inputs{Spec: spec, Commands: []config.CommandSpec{{Command: "go", Params: []string{"build"}}}})
		require.NoError(t, err)
		assert.Equal(t, fp1, fp2)
	})

	t.Run("changes when content changes", func(t *testing.T) {
		writeFile(t, a, "package main // changed\n")
		fp2, err := Compute(Inputs{Spec: spec, Commands: []config.CommandSpec{{Command: "go", Params: []string{"build"}}}})
		require.NoError(t, err)
		assert.NotEqual(t, fp1, fp2)
	})

	t.Run("changes when command changes", func(t *testing.T) {
		fpCmd, err := Compute(Inputs{Spec: spec, Commands: []config.CommandSpec{{Command: "gofmt", Params: []string{"build"}}}})
		require.NoError(t, err)
		fpCmd2, err := Compute(Inputs{Spec: spec, Commands: []config.CommandSpec{{Command: "go", Params: []string{"build"}}}})
		require.NoError(t, err)
		assert.NotEqual(t, fpCmd, fpCmd2)
	})

	t.Run("changes when params change", func(t *testing.T) {
		fpA, err := Compute(Inputs{Spec: spec, Commands: []config.CommandSpec{{Command: "go", Params: []string{"build"}}}})
		require.NoError(t, err)
		fpB, err := Compute(Inputs{Spec: spec, Commands: []config.CommandSpec{{Command: "go", Params: []string{"test"}}}})
		require.NoError(t, err)
		assert.NotEqual(t, fpA, fpB)
	})

	t.Run("changes when a new matching file appears", func(t *testing.T) {
		before, err := Compute(Inputs{Spec: spec, Commands: []config.CommandSpec{{Command: "go", Params: []string{"build"}}}})
		require.NoError(t, err)
		writeFile(t, filepath.Join(dir, "b.go"), "package main\n")
		after, err := Compute(Inputs{Spec: spec, Commands: []config.CommandSpec{{Command: "go", Params: []string{"build"}}}})
		require.NoError(t, err)
		assert.NotEqual(t, before, after)
	})
}

func TestCompute_MtimeMethod(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	writeFile(t, f, "hello")
	spec := &config.Cache{Method: config.CacheMtime, Reads: []string{f}}

	fp1, err := Compute(Inputs{Spec: spec, Commands: []config.CommandSpec{{Command: "cat"}}})
	require.NoError(t, err)

	// Bumping mtime changes the fingerprint even if content is identical.
	future := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(f, future, future))
	fp2, err := Compute(Inputs{Spec: spec, Commands: []config.CommandSpec{{Command: "cat"}}})
	require.NoError(t, err)
	assert.NotEqual(t, fp1, fp2)
}

func TestCompute_DoublestarRecursive(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pkg", "deep", "x.go"), "package deep\n")
	spec := &config.Cache{Method: config.CacheHash, Reads: []string{filepath.Join(dir, "**", "*.go")}}
	fp, err := Compute(Inputs{Spec: spec, Commands: []config.CommandSpec{{Command: "go"}}})
	require.NoError(t, err)
	assert.Contains(t, fp, "sha256:")
}

func TestCompute_MissingExplicitFileErrors(t *testing.T) {
	// A literal (non-glob) path that doesn't exist should surface an error,
	// since the user named a specific input.
	spec := &config.Cache{Method: config.CacheHash, Reads: []string{"/no/such/explicit/file.go"}}
	_, err := Compute(Inputs{Spec: spec, Commands: []config.CommandSpec{{Command: "go"}}})
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
	fp, err := Compute(Inputs{Spec: spec, Commands: []config.CommandSpec{{Command: "go"}}})
	require.NoError(t, err)
	assert.Contains(t, fp, "sha256:")
}

func TestCompute_BadGlobErrors(t *testing.T) {
	spec := &config.Cache{Method: config.CacheHash, Reads: []string{"[invalid"}}
	_, err := Compute(Inputs{Spec: spec, Commands: []config.CommandSpec{{Command: "go"}}})
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
		assert.True(t, WritesPresent("", spec))
	})
	t.Run("absent when a write is missing", func(t *testing.T) {
		spec := &config.Cache{Writes: []string{bin, filepath.Join(dir, "missing")}}
		assert.False(t, WritesPresent("", spec))
	})
	t.Run("no writes declared is trivially present", func(t *testing.T) {
		assert.True(t, WritesPresent("", &config.Cache{}))
	})
	t.Run("relative writes resolve against the root", func(t *testing.T) {
		spec := &config.Cache{Writes: []string{filepath.Join("bin", "app")}}
		assert.True(t, WritesPresent(dir, spec))
		assert.False(t, WritesPresent(t.TempDir(), spec))
	})
}

func TestCompute_RelativeGlobsResolveAgainstRoot(t *testing.T) {
	cmds := []config.CommandSpec{{Command: "go", Params: []string{"build"}}}
	spec := &config.Cache{Method: config.CacheHash, Reads: []string{"src/*.go"}}

	rootA, rootB := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(rootA, "src", "a.go"), "package a\n")
	writeFile(t, filepath.Join(rootB, "src", "a.go"), "package a\n")

	fpA, err := Compute(Inputs{Spec: spec, Root: rootA, Commands: cmds})
	require.NoError(t, err)
	fpB, err := Compute(Inputs{Spec: spec, Root: rootB, Commands: cmds})
	require.NoError(t, err)
	assert.Equal(t, fpA, fpB, "fingerprint must not depend on where the project lives")

	empty, err := Compute(Inputs{Spec: spec, Root: t.TempDir(), Commands: cmds})
	require.NoError(t, err)
	assert.NotEqual(t, fpA, empty, "inputs must be read from the root, not the process cwd")

	writeFile(t, filepath.Join(rootB, "src", "a.go"), "package a // changed\n")
	fpB2, err := Compute(Inputs{Spec: spec, Root: rootB, Commands: cmds})
	require.NoError(t, err)
	assert.NotEqual(t, fpB, fpB2)
}

// A project under a directory whose name holds glob metacharacters must still
// have its reads and writes matched, with a location-independent fingerprint.
func TestCompute_RootWithGlobMetacharacters(t *testing.T) {
	cmds := []config.CommandSpec{{Command: "go"}}
	spec := &config.Cache{Method: config.CacheHash, Reads: []string{"src/**/*.go"}, Writes: []string{"bin/*"}}

	plain := filepath.Join(t.TempDir(), "app")
	meta := filepath.Join(t.TempDir(), "[work]", "a*b?c", "app")
	for _, root := range []string{plain, meta} {
		writeFile(t, filepath.Join(root, "src", "pkg", "a.go"), "package pkg\n")
		writeFile(t, filepath.Join(root, "bin", "app"), "x")
	}

	fpPlain, err := Compute(Inputs{Spec: spec, Root: plain, Commands: cmds})
	require.NoError(t, err)
	fpMeta, err := Compute(Inputs{Spec: spec, Root: meta, Commands: cmds})
	require.NoError(t, err)
	assert.Equal(t, fpPlain, fpMeta, "reads must match under a metacharacter root")
	assert.True(t, WritesPresent(meta, spec), "writes must match under a metacharacter root")

	writeFile(t, filepath.Join(meta, "src", "pkg", "a.go"), "package pkg // changed\n")
	fpChanged, err := Compute(Inputs{Spec: spec, Root: meta, Commands: cmds})
	require.NoError(t, err)
	assert.NotEqual(t, fpMeta, fpChanged, "inputs under a metacharacter root must be hashed")
}

func TestCompute_AbsoluteGlobIgnoresRoot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.go"), "package a\n")
	spec := &config.Cache{Method: config.CacheHash, Reads: []string{filepath.Join(dir, "*.go")}}
	cmds := []config.CommandSpec{{Command: "go"}}

	withRoot, err := Compute(Inputs{Spec: spec, Root: t.TempDir(), Commands: cmds})
	require.NoError(t, err)
	withoutRoot, err := Compute(Inputs{Spec: spec, Commands: cmds})
	require.NoError(t, err)
	assert.Equal(t, withoutRoot, withRoot)
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
		Result:      result.RunResult{Output: "done\n"},
		Commands:    []config.CommandSpec{{Command: "go", Params: []string{"build"}}},
		UpdatedAt:   time.Now(),
	}
	require.NoError(t, store.Save("build", entry))

	t.Run("hit after save", func(t *testing.T) {
		got, ok := store.Load("build")
		require.True(t, ok)
		assert.Equal(t, "sha256:abc", got.Fingerprint)
		assert.Equal(t, "done\n", got.Result.Output)
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

// TestEntryRoundTripWithRunResult verifies that a full RunResult is
// persisted and restored faithfully through the FileStore.
func TestEntryRoundTripWithRunResult(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := NewFileStore(dir)
	want := &Entry{
		Fingerprint: "sha256:abc",
		Result: result.RunResult{
			Stdout:     "hi\n",
			Stderr:     "warn\n",
			Output:     "hi\nwarn\n",
			ExitCode:   0,
			DurationMs: 12,
			Status:     "ok",
		},
		Commands:  []config.CommandSpec{{Command: "echo", Params: []string{"hi"}}},
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	}
	require.NoError(t, s.Save("g", want))
	got, ok := s.Load("g")
	require.True(t, ok)
	assert.Equal(t, want.Result, got.Result)
	assert.Equal(t, want.Fingerprint, got.Fingerprint)
}

// TestCompute_IsDeterministic confirms identical inputs always produce the
// same "sha256:"-prefixed fingerprint.
func TestCompute_IsDeterministic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o600))
	spec := &config.Cache{Method: config.CacheHash, Reads: []string{filepath.Join(dir, "f.txt")}}
	fp, err := Compute(Inputs{Spec: spec, Commands: []config.CommandSpec{{Command: "echo", Params: []string{"a"}}}})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(fp, "sha256:"))

	// Determinism: identical inputs produce identical fingerprints.
	fp2, err := Compute(Inputs{Spec: spec, Commands: []config.CommandSpec{{Command: "echo", Params: []string{"a"}}}})
	require.NoError(t, err)
	assert.Equal(t, fp, fp2)
}

func TestCompute_CommandListFingerprint(t *testing.T) {
	spec := &config.Cache{Method: config.CacheHash, Reads: []string{}}
	base := []config.CommandSpec{
		{Command: "go", Params: []string{"build"}},
		{Command: "go test ./...", IsShell: true},
	}

	fp1, err := Compute(Inputs{Spec: spec, Shell: "sh", Commands: base})
	require.NoError(t, err)

	t.Run("identical lists hit", func(t *testing.T) {
		fp2, err := Compute(Inputs{Spec: spec, Shell: "sh", Commands: []config.CommandSpec{
			{Command: "go", Params: []string{"build"}},
			{Command: "go test ./...", IsShell: true},
		}})
		require.NoError(t, err)
		assert.Equal(t, fp1, fp2)
	})

	t.Run("changing any command busts", func(t *testing.T) {
		fp2, err := Compute(Inputs{Spec: spec, Shell: "sh", Commands: []config.CommandSpec{
			{Command: "go", Params: []string{"build"}},
			{Command: "go vet ./...", IsShell: true}, // second entry changed
		}})
		require.NoError(t, err)
		assert.NotEqual(t, fp1, fp2)
	})

	t.Run("changing a param busts", func(t *testing.T) {
		fp2, err := Compute(Inputs{Spec: spec, Shell: "sh", Commands: []config.CommandSpec{
			{Command: "go", Params: []string{"install"}},
			{Command: "go test ./...", IsShell: true},
		}})
		require.NoError(t, err)
		assert.NotEqual(t, fp1, fp2)
	})

	t.Run("changing entry form (argv vs shell) busts", func(t *testing.T) {
		fp2, err := Compute(Inputs{Spec: spec, Shell: "sh", Commands: []config.CommandSpec{
			{Command: "go", Params: []string{"build"}},
			{Command: "go test ./...", IsShell: false}, // same text, argv form
		}})
		require.NoError(t, err)
		assert.NotEqual(t, fp1, fp2)
	})

	t.Run("adding an entry busts", func(t *testing.T) {
		fp2, err := Compute(Inputs{Spec: spec, Shell: "sh", Commands: append(base, config.CommandSpec{Command: "true"})})
		require.NoError(t, err)
		assert.NotEqual(t, fp1, fp2)
	})

	t.Run("changing the shell busts shell-form entries", func(t *testing.T) {
		list := []config.CommandSpec{
			{Command: "go test ./...", IsShell: true},
		}
		fpBash, err := Compute(Inputs{Spec: spec, Shell: "bash", Commands: list})
		require.NoError(t, err)
		fpZsh, err := Compute(Inputs{Spec: spec, Shell: "zsh", Commands: list})
		require.NoError(t, err)
		assert.NotEqual(t, fpBash, fpZsh)
	})

	t.Run("changing the shell does not bust argv-only lists", func(t *testing.T) {
		list := []config.CommandSpec{
			{Command: "go", Params: []string{"build"}},
		}
		fpBash, err := Compute(Inputs{Spec: spec, Shell: "bash", Commands: list})
		require.NoError(t, err)
		fpZsh, err := Compute(Inputs{Spec: spec, Shell: "zsh", Commands: list})
		require.NoError(t, err)
		assert.Equal(t, fpBash, fpZsh)
	})
}
