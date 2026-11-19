package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dirNames lists the entry names in dir.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestFileStore_SaveLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)
	for range 3 {
		require.NoError(t, store.Save("build", &Entry{Fingerprint: "sha256:abc"}))
	}
	assert.Equal(t, []string{"build.json"}, dirNames(t, dir))
}

func TestFileStore_FailedSaveCleansUpTempFile(t *testing.T) {
	dir := t.TempDir()
	// A non-empty directory where the entry belongs makes the final rename fail.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "build.json", "occupied"), 0o755))
	store := NewFileStore(dir)

	err := store.Save("build", &Entry{Fingerprint: "sha256:abc"})
	require.Error(t, err)
	assert.Equal(t, []string{"build.json"}, dirNames(t, dir), "temp file must be removed on failure")
}

// A reader that opened the entry before a Save must keep reading the complete
// old entry. Writing the live file in place (truncate, then write) would hand
// that reader a partial or mixed entry; replacing it by rename never does.
func TestFileStore_SaveNeverRewritesTheLiveEntryInPlace(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)
	require.NoError(t, store.Save("build", &Entry{Fingerprint: "sha256:old"}))

	reader, err := os.Open(filepath.Join(dir, "build.json"))
	require.NoError(t, err)
	defer reader.Close()

	require.NoError(t, store.Save("build", &Entry{Fingerprint: "sha256:new"}))

	var seen Entry
	require.NoError(t, json.NewDecoder(reader).Decode(&seen))
	assert.Equal(t, "sha256:old", seen.Fingerprint, "in-flight reader must see the complete previous entry")

	got, ok := store.Load("build")
	require.True(t, ok)
	assert.Equal(t, "sha256:new", got.Fingerprint)
}
