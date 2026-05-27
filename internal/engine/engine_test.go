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
	"github.com/quike/keepup/internal/template"
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

// stepFlowCfg builds a config with a single step-mode flow over the given groups.
func stepFlowCfg(t *testing.T, groups []config.Group, steps [][]string) *config.Config {
	t.Helper()
	flow := config.Flow{Mode: config.ModeStep}
	for _, s := range steps {
		flow.Steps = append(flow.Steps, config.Step{Run: append([]string(nil), s...)})
	}
	return &config.Config{
		Version: config.SchemaVersion,
		Groups:  groups,
		Flows:   map[string]config.Flow{"f": flow},
	}
}

func dagFlowCfg(_ *testing.T, groups []config.Group, run []string) *config.Config {
	return &config.Config{
		Version: config.SchemaVersion,
		Groups:  groups,
		Flows: map[string]config.Flow{
			"f": {Mode: config.ModeDAG, Run: append([]string(nil), run...)},
		},
	}
}

func TestEngine_StepFlow_TwoGroupsInOneStep(t *testing.T) {
	cfg := stepFlowCfg(t, []config.Group{
		{Name: "a", Command: "echo"},
		{Name: "b", Command: "echo"},
	}, [][]string{{"a", "b"}})
	r := &fakeRunner{outputs: map[string]string{"a": "A\n", "b": "B\n"}}
	e := New(cfg, WithRunner(r))

	require.NoError(t, e.RunFlow(context.Background(), "f"))
	got, _ := e.Outputs().Get("a")
	assert.Equal(t, "A\n", got)
}

func TestEngine_StepFlow_RunnerErrorPropagatesWrapped(t *testing.T) {
	boom := errors.New("boom")
	cfg := stepFlowCfg(t, []config.Group{{Name: "a", Command: "false"}}, [][]string{{"a"}})
	e := New(cfg, WithRunner(&fakeRunner{errs: map[string]error{"a": boom}}))
	err := e.RunFlow(context.Background(), "f")
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
	assert.Contains(t, err.Error(), "step 1")
}

func TestEngine_DryRunSkipsRunner(t *testing.T) {
	cfg := stepFlowCfg(t, []config.Group{{Name: "a", Command: "rm"}}, [][]string{{"a"}})
	cfg.Settings.DryRun = true
	r := &fakeRunner{}
	e := New(cfg, WithRunner(r))
	require.NoError(t, e.RunFlow(context.Background(), "f"))
	assert.Empty(t, r.calls)
}

func TestEngine_RunFlow_UnknownFlow(t *testing.T) {
	cfg := stepFlowCfg(t, []config.Group{{Name: "a", Command: "echo"}}, [][]string{{"a"}})
	e := New(cfg, WithRunner(&fakeRunner{}))
	err := e.RunFlow(context.Background(), "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestEngine_RunFlow_DefaultUsedWhenEmpty(t *testing.T) {
	cfg := stepFlowCfg(t, []config.Group{{Name: "a", Command: "echo"}}, [][]string{{"a"}})
	cfg.Default = "f"
	r := &fakeRunner{outputs: map[string]string{"a": "A"}}
	e := New(cfg, WithRunner(r))
	require.NoError(t, e.RunFlow(context.Background(), ""))
	assert.Equal(t, []string{"a:"}, r.calls)
}

func TestEngine_RunFlow_EmptyAndNoDefaultErrors(t *testing.T) {
	cfg := stepFlowCfg(t, []config.Group{{Name: "a", Command: "echo"}}, [][]string{{"a"}})
	e := New(cfg, WithRunner(&fakeRunner{}))
	err := e.RunFlow(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no flow specified")
}

func TestEngine_StepFlow_CrossStepOutputExpansion(t *testing.T) {
	cfg := stepFlowCfg(t, []config.Group{
		{Name: "producer", Command: "echo", Params: []string{"banana"}},
		{Name: "consumer", Command: "echo", Params: []string{"{{ output.producer }}"}},
	}, [][]string{{"producer"}, {"consumer"}})
	r := &fakeRunner{outputs: map[string]string{"producer": "banana\n"}}
	e := New(cfg, WithRunner(r))

	require.NoError(t, e.RunFlow(context.Background(), "f"))
	require.Len(t, r.calls, 2)
	assert.Equal(t, "consumer:banana", r.calls[1])
}

func TestEngine_ContextCancellationAborts(t *testing.T) {
	cfg := stepFlowCfg(t, []config.Group{{Name: "slow", Command: "sleep"}}, [][]string{{"slow"}})
	r := &fakeRunner{delays: map[string]time.Duration{"slow": 5 * time.Second}}
	e := New(cfg, WithRunner(r))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.Error(t, e.RunFlow(ctx, "f"))
}

func TestEngine_MaxConcurrencyCapsParallelism(t *testing.T) {
	const n = 5
	groups := make([]config.Group, n)
	wave := make([]string, n)
	for i := range n {
		name := string(rune('a' + i))
		groups[i] = config.Group{Name: name, Command: "echo"}
		wave[i] = name
	}
	cfg := stepFlowCfg(t, groups, [][]string{wave})
	cfg.Settings.MaxConcurrency = 1

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
		after: func() { mu.Lock(); active--; mu.Unlock() },
	}
	e := New(cfg, WithRunner(r))
	require.NoError(t, e.RunFlow(context.Background(), "f"))
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

func TestEngine_DAGFlow_ProducerThenConsumer(t *testing.T) {
	cfg := dagFlowCfg(t, []config.Group{
		{Name: "producer", Command: "echo", Params: []string{"x"}},
		{Name: "consumer", Command: "echo", Params: []string{"{{ output.producer }}"}},
	}, []string{"producer", "consumer"})
	r := &fakeRunner{outputs: map[string]string{"producer": "banana\n"}}
	e := New(cfg, WithRunner(r))

	require.NoError(t, e.RunFlow(context.Background(), "f"))
	// Consumer must have been called AFTER producer with the expanded value.
	require.Len(t, r.calls, 2)
	assert.Equal(t, "producer:x", r.calls[0])
	assert.Equal(t, "consumer:banana", r.calls[1])
}

func TestEngine_DAGFlow_ParallelIndependentRoots(t *testing.T) {
	// a and b have no data deps; consumer depends on both. We assert that
	// (i) consumer runs last and (ii) it sees both outputs.
	cfg := dagFlowCfg(t, []config.Group{
		{Name: "a", Command: "echo"},
		{Name: "b", Command: "echo"},
		{Name: "consumer", Command: "echo", Params: []string{"{{ output.a }}/{{ output.b }}"}},
	}, []string{"a", "b", "consumer"})
	r := &fakeRunner{outputs: map[string]string{"a": "A\n", "b": "B\n"}}
	e := New(cfg, WithRunner(r))

	require.NoError(t, e.RunFlow(context.Background(), "f"))
	require.Len(t, r.calls, 3)
	last := r.calls[2]
	assert.True(t, strings.HasPrefix(last, "consumer:"))
	assert.Contains(t, last, "A/B")
}

func TestEngine_Options(t *testing.T) {
	cfg := stepFlowCfg(t, []config.Group{{Name: "a", Command: "echo"}}, [][]string{{"a"}})

	t.Run("WithOutputStore is honored", func(t *testing.T) {
		custom := NewMemoryOutputStore()
		custom.Set("seed", "preloaded")
		e := New(cfg, WithOutputStore(custom), WithRunner(&fakeRunner{}))
		assert.Same(t, custom, e.Outputs())
	})

	t.Run("WithLogger replaces the default Nop", func(t *testing.T) {
		captured := &captureLogger{}
		e := New(cfg, WithLogger(captured), WithRunner(&fakeRunner{outputs: map[string]string{"a": ""}}))
		require.NoError(t, e.RunFlow(context.Background(), "f"))
		assert.NotEmpty(t, captured.lines)
	})

	t.Run("WithDryRun forces dry-run regardless of config", func(t *testing.T) {
		r := &fakeRunner{}
		e := New(cfg, WithDryRun(true), WithRunner(r))
		require.NoError(t, e.RunFlow(context.Background(), "f"))
		assert.Empty(t, r.calls)
	})

	t.Run("WithExpander is honored", func(t *testing.T) {
		called := false
		var exp expanderFunc = func(s string, _ template.Data) (string, error) {
			called = true
			return s, nil
		}
		cfg2 := stepFlowCfg(t, []config.Group{{Name: "a", Command: "echo", Params: []string{"hi"}}}, [][]string{{"a"}})
		e := New(cfg2, WithExpander(exp), WithRunner(&fakeRunner{outputs: map[string]string{"a": ""}}))
		require.NoError(t, e.RunFlow(context.Background(), "f"))
		assert.True(t, called)
	})
}

type expanderFunc func(string, template.Data) (string, error)

func (f expanderFunc) Expand(s string, d template.Data) (string, error) { return f(s, d) }

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
	snap["a"] = "mutated"
	v, _ := s.Get("a")
	assert.Equal(t, "1", v)
}
