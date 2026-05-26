package watch

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSource lets tests drive events deterministically without real I/O.
type fakeSource struct {
	events chan Event
	errs   chan error
	mu     sync.Mutex
	added  []string
}

func newFakeSource() *fakeSource {
	return &fakeSource{events: make(chan Event, 16), errs: make(chan error, 4)}
}

func (s *fakeSource) Events() <-chan Event { return s.events }
func (s *fakeSource) Errors() <-chan error { return s.errs }
func (s *fakeSource) Add(p string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.added = append(s.added, p)
	return nil
}
func (s *fakeSource) Close() error { return nil }

func TestWatcher_InitialRun(t *testing.T) {
	t.Parallel()
	src := newFakeSource()
	var runs int32
	w := New([]string{"*.go"}, src, WithDebounce(10*time.Millisecond))

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		_ = w.Run(ctx, func(context.Context) error {
			atomic.AddInt32(&runs, 1)
			return nil
		})
		close(done)
	}()

	// The initial run should fire without any event.
	require.Eventually(t, func() bool { return atomic.LoadInt32(&runs) >= 1 }, time.Second, 5*time.Millisecond)
	cancel()
	<-done
}

func TestWatcher_RerunsOnMatchingChange(t *testing.T) {
	t.Parallel()
	src := newFakeSource()
	var runs int32
	w := New([]string{"*.go"}, src, WithDebounce(10*time.Millisecond), WithInitialRun(false))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = w.Run(ctx, func(context.Context) error { atomic.AddInt32(&runs, 1); return nil }) }()

	src.events <- Event{Path: "main.go"}
	require.Eventually(t, func() bool { return atomic.LoadInt32(&runs) == 1 }, time.Second, 5*time.Millisecond)
}

func TestWatcher_IgnoresNonMatchingChange(t *testing.T) {
	t.Parallel()
	src := newFakeSource()
	var runs int32
	w := New([]string{"*.go"}, src, WithDebounce(10*time.Millisecond), WithInitialRun(false))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = w.Run(ctx, func(context.Context) error { atomic.AddInt32(&runs, 1); return nil }) }()

	src.events <- Event{Path: "README.md"}
	// Give the loop time to (not) react.
	time.Sleep(60 * time.Millisecond)
	assert.Equal(t, int32(0), atomic.LoadInt32(&runs))
}

func TestWatcher_DebouncesBurst(t *testing.T) {
	t.Parallel()
	src := newFakeSource()
	var runs int32
	w := New([]string{"*.go"}, src, WithDebounce(40*time.Millisecond), WithInitialRun(false))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = w.Run(ctx, func(context.Context) error { atomic.AddInt32(&runs, 1); return nil }) }()

	// A rapid burst should collapse into a single run.
	for range 5 {
		src.events <- Event{Path: "main.go"}
		time.Sleep(5 * time.Millisecond)
	}
	require.Eventually(t, func() bool { return atomic.LoadInt32(&runs) == 1 }, time.Second, 5*time.Millisecond)
	// Ensure it stays at exactly one for a bit.
	time.Sleep(60 * time.Millisecond)
	assert.Equal(t, int32(1), atomic.LoadInt32(&runs))
}

func TestWatcher_FailingOnChangeKeepsWatching(t *testing.T) {
	t.Parallel()
	src := newFakeSource()
	var runs int32
	w := New([]string{"*.go"}, src, WithDebounce(10*time.Millisecond), WithInitialRun(false))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		_ = w.Run(ctx, func(context.Context) error {
			atomic.AddInt32(&runs, 1)
			return errors.New("boom")
		})
	}()

	src.events <- Event{Path: "a.go"}
	require.Eventually(t, func() bool { return atomic.LoadInt32(&runs) == 1 }, time.Second, 5*time.Millisecond)
	// A second change still triggers another run despite the first failing.
	src.events <- Event{Path: "b.go"}
	require.Eventually(t, func() bool { return atomic.LoadInt32(&runs) == 2 }, time.Second, 5*time.Millisecond)
}

func TestWatcher_StopsOnContextCancel(t *testing.T) {
	t.Parallel()
	src := newFakeSource()
	w := New([]string{"*.go"}, src, WithInitialRun(false))

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx, func(context.Context) error { return nil }) }()

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

func TestWatcher_SourceErrorIsNonFatal(t *testing.T) {
	t.Parallel()
	src := newFakeSource()
	var runs int32
	w := New([]string{"*.go"}, src, WithDebounce(10*time.Millisecond), WithInitialRun(false))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = w.Run(ctx, func(context.Context) error { atomic.AddInt32(&runs, 1); return nil }) }()

	src.errs <- errors.New("transient")
	src.events <- Event{Path: "main.go"}
	require.Eventually(t, func() bool { return atomic.LoadInt32(&runs) == 1 }, time.Second, 5*time.Millisecond)
}
