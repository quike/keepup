package engine

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/result"
)

// exitRunner fails every command with the given exit code, as a real process
// exiting non-zero would.
type exitRunner struct{ code int }

func (r exitRunner) Run(_ context.Context, _ *config.Group, _ config.CommandSpec, _ map[string]string) (result.RunResult, error) {
	return result.RunResult{Output: "boom\n", ExitCode: r.code, Status: result.StatusOK}, errors.New("exit status")
}

func TestEngine_GroupFailure_IsTypedWithExitCode(t *testing.T) {
	t.Parallel()
	for name, cfg := range map[string]*config.Config{
		"step": stepFlowCfg(t, []config.Group{{Name: "a", Command: "x"}}, [][]string{{"a"}}),
		"dag":  dagFlowCfg(t, []config.Group{{Name: "a", Command: "x"}}, []string{"a"}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := New(cfg, WithRunner(exitRunner{code: 7})).RunFlow(context.Background(), "f")

			var gf *GroupFailedError
			require.ErrorAs(t, err, &gf)
			assert.Equal(t, "a", gf.Group)
			assert.Equal(t, 7, gf.ExitCode)
		})
	}
}

func TestEngine_GroupFailure_StoresFailedResult(t *testing.T) {
	t.Parallel()
	cfg := stepFlowCfg(t, []config.Group{{Name: "a", Command: "x"}}, [][]string{{"a"}})
	e := New(cfg, WithRunner(exitRunner{code: 3}))
	require.Error(t, e.RunFlow(context.Background(), "f"))

	got, ok := e.Outputs().Get("a")
	require.True(t, ok, "a failed group must still be recorded")
	assert.Equal(t, result.StatusFailed, got.Status)
	assert.Equal(t, 3, got.ExitCode)
	assert.Equal(t, "boom\n", got.Output)
}

func TestEngine_GroupEndEventCarriesExitCode(t *testing.T) {
	t.Parallel()
	groups := []config.Group{{Name: "ok", Command: "x"}, {Name: "bad", Command: "x"}}
	cfg := stepFlowCfg(t, groups, [][]string{{"ok"}, {"bad"}})
	runner := &fakeRunner{errs: map[string]error{"bad": errors.New("exit status 1")}}
	var buf bytes.Buffer
	e := New(cfg, WithRunner(runner), WithEmitter(NewJSONEmitter(&buf)))
	require.Error(t, e.RunFlow(context.Background(), "f"))

	evs := decodeEvents(t, buf.Bytes())
	assert.Equal(t, new(0), exitCodeOf(evs, "ok"), "a group that ran reports its exit code, even 0")
	assert.Equal(t, new(1), exitCodeOf(evs, "bad"))
}

func TestEngine_GroupEndEventOmitsExitCodeWhenNothingRan(t *testing.T) {
	t.Parallel()
	cfg := stepFlowCfg(t, []config.Group{{Name: "a", Command: "x"}}, [][]string{{"a"}})
	var buf bytes.Buffer
	e := New(cfg, WithRunner(&fakeRunner{}), WithDryRun(true), WithEmitter(NewJSONEmitter(&buf)))
	require.NoError(t, e.RunFlow(context.Background(), "f"))

	assert.NotContains(t, buf.String(), "exitCode")
}

func TestEngine_Timeout_IsTyped(t *testing.T) {
	t.Parallel()
	cfg := stepFlowCfg(t, []config.Group{{Name: "a", Command: "x"}}, [][]string{{"a"}})
	withStepEnvelope(cfg, "20ms", 0)

	err := New(cfg, WithRunner(&blockingRunner{})).RunFlow(context.Background(), "f")

	var te *TimeoutError
	require.ErrorAs(t, err, &te)
	assert.Equal(t, 20*time.Millisecond, te.Timeout)
	var ce *CanceledError
	assert.NotErrorAs(t, err, &ce, "a timeout is not a cancellation")
}

func TestEngine_FailureAfterEarlierTimeout_IsNotATimeout(t *testing.T) {
	t.Parallel()
	cfg := stepFlowCfg(t, []config.Group{{Name: "a", Command: "x"}}, [][]string{{"a"}})
	withStepEnvelope(cfg, "20ms", 1)

	r := &timeoutThenFailRunner{}
	err := New(cfg, WithRunner(r), WithRetryBackoff(time.Millisecond)).RunFlow(context.Background(), "f")

	var te *TimeoutError
	assert.NotErrorAs(t, err, &te, "only the final attempt decides the failure kind")
	var gf *GroupFailedError
	require.ErrorAs(t, err, &gf)
	assert.Equal(t, 5, gf.ExitCode)
}

// timeoutThenFailRunner blocks until its deadline on the first call, then
// fails fast with exit code 5.
type timeoutThenFailRunner struct{ calls int }

func (r *timeoutThenFailRunner) Run(
	ctx context.Context, _ *config.Group, _ config.CommandSpec, _ map[string]string,
) (result.RunResult, error) {
	r.calls++
	if r.calls == 1 {
		<-ctx.Done()
		return result.RunResult{}, ctx.Err()
	}
	return result.RunResult{ExitCode: 5}, errors.New("exit status 5")
}

func TestEngine_Cancellation_IsTyped(t *testing.T) {
	t.Parallel()
	for name, cfg := range map[string]*config.Config{
		"step": stepFlowCfg(t, []config.Group{{Name: "a", Command: "x"}}, [][]string{{"a"}}),
		"dag":  dagFlowCfg(t, []config.Group{{Name: "a", Command: "x"}}, []string{"a"}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := cfg.Flows["f"]
			f.Timeout = "10s" // an armed timeout must not mask a cancel
			cfg.Flows["f"] = f
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(20*time.Millisecond, cancel)

			err := New(cfg, WithRunner(&blockingRunner{})).RunFlow(ctx, "f")

			var ce *CanceledError
			require.ErrorAs(t, err, &ce)
			assert.ErrorIs(t, ce.Cause, context.Canceled)
			var te *TimeoutError
			assert.NotErrorAs(t, err, &te)
		})
	}
}

func TestEngine_UnknownFlow_IsConfigError(t *testing.T) {
	t.Parallel()
	cfg := stepFlowCfg(t, []config.Group{{Name: "a", Command: "x"}}, [][]string{{"a"}})
	for _, flow := range []string{"missing", ""} {
		err := New(cfg, WithRunner(&fakeRunner{})).RunFlow(context.Background(), flow)
		var ce *config.Error
		assert.ErrorAs(t, err, &ce, "flow %q", flow)
	}
}

// exitCodeOf returns the exitCode on a group's group.end event.
func exitCodeOf(evs []Event, group string) *int {
	for i := range evs {
		if evs[i].Event == EventGroupEnd && evs[i].Group == group {
			return evs[i].ExitCode
		}
	}
	return nil
}
