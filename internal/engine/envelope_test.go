package engine

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/result"
)

// flakyRunner fails the first failUntil-1 attempts, then succeeds.
type flakyRunner struct {
	failUntil int32 // number of attempts that should fail
	calls     int32
	output    string
}

func (r *flakyRunner) Run(_ context.Context, _ *config.Group, _ []string, _ map[string]string) (result.RunResult, error) {
	n := atomic.AddInt32(&r.calls, 1)
	if n <= r.failUntil {
		return result.RunResult{ExitCode: 1}, errors.New("transient failure")
	}
	return result.RunResult{Stdout: r.output, Output: r.output, Status: "ok"}, nil
}

// blockingRunner blocks until ctx is done, then returns ctx.Err() — used to
// exercise timeouts.
type blockingRunner struct{ calls int32 }

func (r *blockingRunner) Run(ctx context.Context, _ *config.Group, _ []string, _ map[string]string) (result.RunResult, error) {
	atomic.AddInt32(&r.calls, 1)
	<-ctx.Done()
	return result.RunResult{}, ctx.Err()
}

func TestResolveEnvelope(t *testing.T) {
	t.Parallel()
	flow := &config.Flow{Timeout: "5s", Retries: 2}

	t.Run("flow defaults when step is nil (dag)", func(t *testing.T) {
		env := resolveEnvelope(flow, nil)
		assert.Equal(t, 5*time.Second, env.timeout)
		assert.Equal(t, 2, env.retries)
	})

	t.Run("step overrides flow", func(t *testing.T) {
		env := resolveEnvelope(flow, &config.Step{Timeout: "1s", Retries: 5})
		assert.Equal(t, time.Second, env.timeout)
		assert.Equal(t, 5, env.retries)
	})

	t.Run("step inherits flow when unset", func(t *testing.T) {
		env := resolveEnvelope(flow, &config.Step{})
		assert.Equal(t, 5*time.Second, env.timeout)
		assert.Equal(t, 2, env.retries)
	})

	t.Run("no envelope at all", func(t *testing.T) {
		env := resolveEnvelope(&config.Flow{}, &config.Step{})
		assert.Equal(t, time.Duration(0), env.timeout)
		assert.Equal(t, 0, env.retries)
	})
}

func TestEngine_Retries_SucceedAfterFailures(t *testing.T) {
	t.Parallel()
	cfg := stepFlowCfg(t, []config.Group{{Name: "a", Command: "x"}}, [][]string{{"a"}})
	withStepEnvelope(cfg, "", 2) // 2 retries → up to 3 attempts

	r := &flakyRunner{failUntil: 2, output: "ok"}
	e := New(cfg, WithRunner(r), WithRetryBackoff(time.Millisecond))
	require.NoError(t, e.RunFlow(context.Background(), "f"))
	assert.Equal(t, int32(3), atomic.LoadInt32(&r.calls), "should retry until the 3rd attempt succeeds")
	v, _ := e.Outputs().Get("a")
	assert.Equal(t, "ok", v.Output)
}

func TestEngine_Retries_ExhaustedFails(t *testing.T) {
	t.Parallel()
	cfg := stepFlowCfg(t, []config.Group{{Name: "a", Command: "x"}}, [][]string{{"a"}})
	withStepEnvelope(cfg, "", 1) // 1 retry → 2 attempts

	r := &flakyRunner{failUntil: 99, output: "never"}
	e := New(cfg, WithRunner(r), WithRetryBackoff(time.Millisecond))
	err := e.RunFlow(context.Background(), "f")
	require.Error(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&r.calls), "should attempt exactly 1+retries times")
}

func TestEngine_Timeout_Fires(t *testing.T) {
	t.Parallel()
	cfg := stepFlowCfg(t, []config.Group{{Name: "a", Command: "x"}}, [][]string{{"a"}})
	withStepEnvelope(cfg, "20ms", 0)

	r := &blockingRunner{}
	e := New(cfg, WithRunner(r))
	start := time.Now()
	err := e.RunFlow(context.Background(), "f")
	require.Error(t, err)
	assert.Less(t, time.Since(start), 2*time.Second, "timeout should cut the run short")
	assert.Equal(t, int32(1), atomic.LoadInt32(&r.calls))
}

func TestEngine_Timeout_WithRetries_EachAttemptBounded(t *testing.T) {
	t.Parallel()
	cfg := stepFlowCfg(t, []config.Group{{Name: "a", Command: "x"}}, [][]string{{"a"}})
	withStepEnvelope(cfg, "20ms", 2)

	r := &blockingRunner{} // always times out
	e := New(cfg, WithRunner(r), WithRetryBackoff(time.Millisecond))
	err := e.RunFlow(context.Background(), "f")
	require.Error(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&r.calls), "each of 1+retries attempts is timed out and retried")
}

func TestEngine_DAGFlow_UsesFlowEnvelope(t *testing.T) {
	t.Parallel()
	cfg := dagFlowCfg(t, []config.Group{{Name: "a", Command: "x"}}, []string{"a"})
	f := cfg.Flows["f"]
	f.Retries = 1
	cfg.Flows["f"] = f

	r := &flakyRunner{failUntil: 1, output: "ok"}
	e := New(cfg, WithRunner(r), WithRetryBackoff(time.Millisecond))
	require.NoError(t, e.RunFlow(context.Background(), "f"))
	assert.Equal(t, int32(2), atomic.LoadInt32(&r.calls), "dag mode applies the flow-level retries")
}

// TestResolveEnvelope_FromFixture drives resolution from the real
// config-envelope.yml fixture, pinning every edge (inherit, partial override,
// full override, dag flow-level, no-envelope) against an actual file.
func TestResolveEnvelope_FromFixture(t *testing.T) {
	t.Parallel()
	cfg, err := config.LoadConfig("../config/test-resources/config-envelope.yml")
	require.NoError(t, err)

	rel := cfg.Flows["release"]
	t.Run("step inherits both flow defaults", func(t *testing.T) {
		env := resolveEnvelope(&rel, &rel.Steps[0])
		assert.Equal(t, time.Minute, env.timeout)
		assert.Equal(t, 2, env.retries)
	})
	t.Run("step overrides timeout, retries 0 inherits flow", func(t *testing.T) {
		env := resolveEnvelope(&rel, &rel.Steps[1])
		assert.Equal(t, 10*time.Second, env.timeout)
		assert.Equal(t, 2, env.retries, "retries: 0 means inherit, not override-to-zero")
	})
	t.Run("step overrides both", func(t *testing.T) {
		env := resolveEnvelope(&rel, &rel.Steps[2])
		assert.Equal(t, 5*time.Second, env.timeout)
		assert.Equal(t, 5, env.retries)
	})

	t.Run("dag flow uses flow-level envelope", func(t *testing.T) {
		dag := cfg.Flows["fast-dag"]
		env := resolveEnvelope(&dag, nil)
		assert.Equal(t, 30*time.Second, env.timeout)
		assert.Equal(t, 1, env.retries)
	})

	t.Run("bare flow has no envelope", func(t *testing.T) {
		bare := cfg.Flows["bare"]
		env := resolveEnvelope(&bare, &bare.Steps[0])
		assert.Equal(t, time.Duration(0), env.timeout)
		assert.Equal(t, 0, env.retries)
	})
}

// withStepEnvelope sets the timeout/retries envelope on the first step of the
// test flow "f", in place.
func withStepEnvelope(cfg *config.Config, timeout string, retries int) {
	f := cfg.Flows["f"]
	steps := make([]config.Step, len(f.Steps))
	copy(steps, f.Steps)
	steps[0].Timeout = timeout
	steps[0].Retries = retries
	f.Steps = steps
	cfg.Flows["f"] = f
}
