package cmd

import (
	"bytes"
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/watch"
)

func TestWatchPatterns(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Groups: []config.Group{
			{Name: "build", Command: "go", Cache: &config.Cache{Reads: []string{"**/*.go", "go.mod"}}},
			{Name: "test", Command: "go", Cache: &config.Cache{Reads: []string{"**/*.go"}}}, // dup read
			{Name: "lint", Command: "golangci-lint"},                                        // no cache
		},
	}
	flow := &config.Flow{Mode: config.ModeStep, Steps: []config.Step{{Run: []string{"build", "test", "lint"}}}}

	got := watchPatterns(cfg, flow)
	// Deduped, declaration order preserved.
	assert.Equal(t, []string{"**/*.go", "go.mod"}, got)
}

func TestWatchPatterns_NoneWhenNoCache(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Groups: []config.Group{{Name: "a", Command: "echo"}},
	}
	flow := &config.Flow{Mode: config.ModeStep, Steps: []config.Step{{Run: []string{"a"}}}}
	assert.Empty(t, watchPatterns(cfg, flow))
}

func TestWatchCmd_ErrorsWithoutCacheReads(t *testing.T) {
	t.Parallel()
	// A valid flow whose groups declare no cache.reads → watch has nothing to do.
	cfg := `
version: 2
groups:
  - name: a
    command: echo
default: f
flows:
  f:
    mode: step
    steps:
      - run: [a]
`
	cfgPath := writeTempConfig(t, cfg)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"watch", "f", "--config", cfgPath})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no watchable inputs")
}

// stubSource is a minimal watch.Source driven by the test.
type stubSource struct {
	events chan watch.Event
	errs   chan error
	mu     sync.Mutex
	added  []string
}

func newStubSource() *stubSource {
	return &stubSource{events: make(chan watch.Event, 8), errs: make(chan error, 1)}
}

func (s *stubSource) Events() <-chan watch.Event { return s.events }
func (s *stubSource) Errors() <-chan error       { return s.errs }
func (s *stubSource) Add(p string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.added = append(s.added, p)
	return nil
}
func (s *stubSource) Close() error { return nil }

// TestWatch_FixtureDrivesRerun loads the watch test-resource, derives the watch
// set from its cache.reads, and verifies that a change matching one of those
// globs triggers a re-run through the real watch.Watcher.
func TestWatch_FixtureDrivesRerun(t *testing.T) {
	t.Parallel()
	cfg, err := config.LoadConfig("../internal/config/test-resources/config-watch.yml")
	require.NoError(t, err)
	flow := cfg.Flows["dev"]

	patterns := watchPatterns(cfg, &flow)
	// The dev flow's groups declare these reads (deduped, in order).
	assert.Equal(t, []string{"proto/**/*.proto", "**/*.go", "go.mod", "go.sum"}, patterns)

	src := newStubSource()
	w := watch.New(patterns, src, watch.WithDebounce(10*time.Millisecond), watch.WithInitialRun(false))

	var runs int32
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		_ = w.Run(ctx, func(_ context.Context, _ []string) error { atomic.AddInt32(&runs, 1); return nil })
	}()

	// A change to a Go file matches "**/*.go" → should trigger one re-run.
	src.events <- watch.Event{Path: "internal/app/main.go"}
	require.Eventually(t, func() bool { return atomic.LoadInt32(&runs) == 1 },
		time.Second, 5*time.Millisecond)

	// A non-matching change (Markdown) must NOT trigger another run.
	src.events <- watch.Event{Path: "README.md"}
	time.Sleep(40 * time.Millisecond)
	assert.Equal(t, int32(1), atomic.LoadInt32(&runs))
}

func TestWatchCmd_ErrorsOnUnknownFlow(t *testing.T) {
	t.Parallel()
	cfg := `
version: 2
groups:
  - name: a
    command: echo
    cache:
      reads: ["*.go"]
flows:
  f:
    steps:
      - run: [a]
`
	cfgPath := writeTempConfig(t, cfg)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"watch", "ghost", "--config", cfgPath})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
