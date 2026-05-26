package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
)

// fakeRunner records invocations and returns scripted results.
type fakeRunner struct {
	mu      sync.Mutex
	calls   []string
	outputs map[string]string
	errs    map[string]error
	delays  map[string]time.Duration
}

func (f *fakeRunner) Run(ctx context.Context, g *config.Group, params []string, _ map[string]string) (string, error) {
	if d := f.delays[g.Name]; d > 0 {
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, g.Name+":"+strings.Join(params, ","))
	if err, ok := f.errs[g.Name]; ok {
		return f.outputs[g.Name], err
	}
	return f.outputs[g.Name], nil
}

func TestEngine_TwoGroupsInOneStep(t *testing.T) {
	cfg := &config.Config{
		Groups: []config.Group{
			{Name: "a", Command: "echo", Params: []string{"hello"}},
			{Name: "b", Command: "echo", Params: []string{"world"}},
		},
		Execution: []config.Step{{Group: []string{"a", "b"}}},
	}
	r := &fakeRunner{outputs: map[string]string{"a": "A\n", "b": "B\n"}}
	e := New(cfg, WithRunner(r))

	require.NoError(t, e.Run(context.Background()))
	got, _ := e.Outputs().Get("a")
	assert.Equal(t, "A\n", got)
	got, _ = e.Outputs().Get("b")
	assert.Equal(t, "B\n", got)
}

func TestEngine_UndefinedGroupFailsEarly(t *testing.T) {
	cfg := &config.Config{
		Groups:    []config.Group{{Name: "a", Command: "echo"}},
		Execution: []config.Step{{Group: []string{"a", "missing"}}},
	}
	r := &fakeRunner{outputs: map[string]string{"a": "A"}}
	e := New(cfg, WithRunner(r))

	err := e.Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), `group "missing" not defined`)
	assert.Empty(t, r.calls)
}

func TestEngine_RunnerErrorPropagates(t *testing.T) {
	boom := errors.New("boom")
	cfg := &config.Config{
		Groups:    []config.Group{{Name: "a", Command: "false"}},
		Execution: []config.Step{{Group: []string{"a"}}},
	}
	e := New(cfg, WithRunner(&fakeRunner{errs: map[string]error{"a": boom}}))

	err := e.Run(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
	assert.Contains(t, err.Error(), "step 1")
}

func TestEngine_CrossStepOutputExpansion(t *testing.T) {
	cfg := &config.Config{
		Groups: []config.Group{
			{Name: "producer", Command: "echo", Params: []string{"banana"}},
			{Name: "consumer", Command: "echo", Params: []string{"{{ output.producer }}"}},
		},
		Execution: []config.Step{
			{Group: []string{"producer"}},
			{Group: []string{"consumer"}},
		},
	}
	r := &fakeRunner{outputs: map[string]string{"producer": "banana\n", "consumer": "banana\n"}}
	e := New(cfg, WithRunner(r))

	require.NoError(t, e.Run(context.Background()))
	require.Len(t, r.calls, 2)
	assert.Equal(t, "consumer:banana", r.calls[1])
}

func TestEngine_SameStepSiblingsSeeBaselineOnly(t *testing.T) {
	// "a" produces a value but "b" runs concurrently with it; "b" must NOT
	// see "a"'s output during the same step.
	cfg := &config.Config{
		Groups: []config.Group{
			{Name: "a", Command: "echo", Params: []string{"x"}},
			{Name: "b", Command: "echo", Params: []string{"{{ output.a }}"}},
		},
		Execution: []config.Step{{Group: []string{"a", "b"}}},
	}
	r := &fakeRunner{
		outputs: map[string]string{"a": "X\n", "b": ""},
		delays:  map[string]time.Duration{"a": 5 * time.Millisecond},
	}
	e := New(cfg, WithRunner(r))

	require.NoError(t, e.Run(context.Background()))
	var bCall string
	for _, c := range r.calls {
		if strings.HasPrefix(c, "b:") {
			bCall = c
		}
	}
	assert.Equal(t, "b:{{ output.a }}", bCall)
}

func TestEngine_DryRunSkipsRunner(t *testing.T) {
	cfg := &config.Config{
		Settings:  config.Settings{DryRun: true},
		Groups:    []config.Group{{Name: "a", Command: "rm", Params: []string{"-rf", "/"}}},
		Execution: []config.Step{{Group: []string{"a"}}},
	}
	r := &fakeRunner{}
	e := New(cfg, WithRunner(r))

	require.NoError(t, e.Run(context.Background()))
	assert.Empty(t, r.calls, "dry-run must not invoke runner")
}

func TestEngine_ContextCancellationAborts(t *testing.T) {
	cfg := &config.Config{
		Groups:    []config.Group{{Name: "slow", Command: "sleep"}},
		Execution: []config.Step{{Group: []string{"slow"}}},
	}
	r := &fakeRunner{delays: map[string]time.Duration{"slow": 5 * time.Second}}
	e := New(cfg, WithRunner(r))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := e.Run(ctx)
	require.Error(t, err)
}

func TestEngine_MaxConcurrencyCapsParallelism(t *testing.T) {
	const n = 5
	cfg := &config.Config{
		Settings:  config.Settings{MaxConcurrency: 1},
		Execution: []config.Step{{Group: make([]string, n)}},
	}
	for i := range n {
		name := string(rune('a' + i))
		cfg.Groups = append(cfg.Groups, config.Group{Name: name, Command: "echo"})
		cfg.Execution[0].Group[i] = name
	}
	var (
		mu, peakMu sync.Mutex
		active     int
		peak       int
	)
	r := &concurrencyRunner{
		before: func() {
			mu.Lock()
			active++
			peakMu.Lock()
			if active > peak {
				peak = active
			}
			peakMu.Unlock()
			mu.Unlock()
			time.Sleep(2 * time.Millisecond)
		},
		after: func() {
			mu.Lock()
			active--
			mu.Unlock()
		},
	}
	e := New(cfg, WithRunner(r))
	require.NoError(t, e.Run(context.Background()))
	assert.LessOrEqual(t, peak, 1, "max concurrency 1 was violated, peak=%d", peak)
}

type concurrencyRunner struct {
	before func()
	after  func()
}

func (c *concurrencyRunner) Run(_ context.Context, _ *config.Group, _ []string, _ map[string]string) (string, error) {
	c.before()
	defer c.after()
	return "", nil
}

func TestEngine_Options(t *testing.T) {
	cfg := &config.Config{
		Groups:    []config.Group{{Name: "a", Command: "echo"}},
		Execution: []config.Step{{Group: []string{"a"}}},
	}

	custom := NewMemoryOutputStore()
	custom.Set("seed", "preloaded")

	t.Run("WithOutputStore is honored", func(t *testing.T) {
		e := New(cfg, WithOutputStore(custom), WithRunner(&fakeRunner{}))
		assert.Same(t, custom, e.Outputs())
	})

	t.Run("WithExpander is honored", func(t *testing.T) {
		called := false
		var exp expanderFunc = func(s string, _ map[string]string) string {
			called = true
			return s
		}
		e := New(cfg,
			WithExpander(exp),
			WithRunner(&fakeRunner{outputs: map[string]string{"a": ""}}),
		)
		// engine has no template params, so we add one:
		e.cfg.Groups[0].Params = []string{"hello"}
		e.groups["a"] = e.cfg.Groups[0]

		require.NoError(t, e.Run(context.Background()))
		assert.True(t, called)
	})

	t.Run("WithLogger replaces the default Nop", func(t *testing.T) {
		captured := &captureLogger{}
		e := New(cfg,
			WithLogger(captured),
			WithRunner(&fakeRunner{outputs: map[string]string{"a": ""}}),
		)
		require.NoError(t, e.Run(context.Background()))
		assert.NotEmpty(t, captured.lines)
	})

	t.Run("WithDryRun forces dry-run regardless of config", func(t *testing.T) {
		r := &fakeRunner{}
		e := New(cfg, WithDryRun(true), WithRunner(r))
		require.NoError(t, e.Run(context.Background()))
		assert.Empty(t, r.calls)
	})
}

type expanderFunc func(string, map[string]string) string

func (f expanderFunc) Expand(s string, o map[string]string) string { return f(s, o) }

type captureLogger struct {
	mu    sync.Mutex
	lines []string
}

func (c *captureLogger) add(s string)             { c.mu.Lock(); c.lines = append(c.lines, s); c.mu.Unlock() }
func (c *captureLogger) Debug(s string, _ ...any) { c.add("D:" + s) }
func (c *captureLogger) Info(s string, _ ...any)  { c.add("I:" + s) }
func (c *captureLogger) Warn(s string, _ ...any)  { c.add("W:" + s) }
func (c *captureLogger) Error(s string, _ ...any) { c.add("E:" + s) }
func (c *captureLogger) Trace(s string, _ ...any) { c.add("T:" + s) }

func TestOutputStore_Snapshot(t *testing.T) {
	t.Parallel()
	s := NewMemoryOutputStore()
	s.Set("a", "1")
	s.Set("b", "2")
	snap := s.Snapshot()
	assert.Equal(t, "1", snap["a"])
	assert.Equal(t, "2", snap["b"])
	// Mutating the snapshot must not affect the store.
	snap["a"] = "mutated"
	v, _ := s.Get("a")
	assert.Equal(t, "1", v)
}
