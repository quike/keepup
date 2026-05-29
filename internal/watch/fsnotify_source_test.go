package watch

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestFSNotifySource_RealFileChange exercises the production fsnotify source
// end-to-end: a real write under a watched directory must drive a re-run.
// Uses generous timeouts to stay robust across platforms.
func TestFSNotifySource_RealFileChange(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.go")
	require.NoError(t, os.WriteFile(target, []byte("package main\n"), 0o600))

	src, err := NewFSNotifySource()
	require.NoError(t, err)
	defer func() { _ = src.Close() }()
	require.NoError(t, src.Add(dir))

	var runs int32
	w := New([]string{filepath.Join(dir, "*.go")}, src,
		WithDebounce(30*time.Millisecond), WithInitialRun(false))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		_ = w.Run(ctx, func(_ context.Context, _ []string) error { atomic.AddInt32(&runs, 1); return nil })
	}()

	// Give the watcher a moment to settle, then modify the file.
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, os.WriteFile(target, []byte("package main // edit\n"), 0o600))

	require.Eventually(t, func() bool { return atomic.LoadInt32(&runs) >= 1 },
		3*time.Second, 20*time.Millisecond, "expected a re-run after a real file change")
}

func TestFSNotifySource_AddMissingDirErrors(t *testing.T) {
	src, err := NewFSNotifySource()
	require.NoError(t, err)
	defer func() { _ = src.Close() }()
	require.Error(t, src.Add(filepath.Join(t.TempDir(), "does-not-exist")))
}
