package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/result"
	"github.com/quike/keepup/internal/template"
)

// runSeq drives runSequence directly with a scripted runner so each failure
// policy can be asserted on the commands that actually ran.
func runSeq(t *testing.T, commands []config.CommandSpec, errs map[string]error) (*specRunner, error) {
	t.Helper()
	r := &specRunner{errs: errs, outputs: map[string]string{}}
	e := New(&config.Config{Version: 2}, WithRunner(r))
	g := &config.Group{Name: "g", Commands: commands}
	_, err := e.runSequence(context.Background(), g, commands)
	return r, err
}

func TestRunSequence_StopsAtFirstFailureByDefault(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	r, err := runSeq(t, []config.CommandSpec{
		{Command: "setup"}, {Command: "test"}, {Command: "deploy"},
	}, map[string]error{"test": boom})

	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
	assert.Equal(t, []string{"setup", "test"}, r.commandSeq())
}

func TestRunSequence_AlwaysRunsAfterFailure(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	r, err := runSeq(t, []config.CommandSpec{
		{Command: "setup"},
		{Command: "test"},
		{Command: "skipped"},
		{Command: "teardown", Always: true},
	}, map[string]error{"test": boom})

	require.Error(t, err, "always must not forgive the original failure")
	assert.ErrorIs(t, err, boom)
	assert.Equal(t, []string{"setup", "test", "teardown"}, r.commandSeq(),
		"entries after a failure are skipped unless always")
}

func TestRunSequence_ContinueOnErrorTolerates(t *testing.T) {
	t.Parallel()
	r, err := runSeq(t, []config.CommandSpec{
		{Command: "setup"},
		{Command: "lint", ContinueOnError: true},
		{Command: "deploy"},
	}, map[string]error{"lint": errors.New("lint failed")})

	require.NoError(t, err, "a tolerated failure must not fail the group")
	assert.Equal(t, []string{"setup", "lint", "deploy"}, r.commandSeq())
}

// Fully-tolerated semantics: the group reports success, so its result must not
// carry the soft failure's exit code.
func TestRunSequence_ContinueOnErrorLeavesResultClean(t *testing.T) {
	t.Parallel()
	r := &specRunner{errs: map[string]error{"lint": errors.New("nope")}, outputs: map[string]string{}}
	e := New(&config.Config{Version: 2}, WithRunner(r))
	commands := []config.CommandSpec{{Command: "lint", ContinueOnError: true}}

	out, err := e.runSequence(context.Background(), &config.Group{Name: "g"}, commands)
	require.NoError(t, err)
	assert.Zero(t, out.ExitCode)
	assert.Equal(t, "ok", out.Status)
}

func TestRunSequence_AlwaysFailureKeepsFirstError(t *testing.T) {
	t.Parallel()
	first := errors.New("the real cause")
	r, err := runSeq(t, []config.CommandSpec{
		{Command: "test"},
		{Command: "teardown", Always: true},
	}, map[string]error{"test": first, "teardown": errors.New("cleanup also broke")})

	require.Error(t, err)
	assert.ErrorIs(t, err, first, "the original failure must win over the teardown's")
	assert.NotContains(t, err.Error(), "cleanup also broke")
	assert.Equal(t, []string{"test", "teardown"}, r.commandSeq())
}

func TestRunSequence_AlwaysWithContinueOnErrorSucceeds(t *testing.T) {
	t.Parallel()
	r, err := runSeq(t, []config.CommandSpec{
		{Command: "test"},
		{Command: "cleanup", Always: true, ContinueOnError: true},
	}, map[string]error{"cleanup": errors.New("cleanup broke")})

	require.NoError(t, err, "a tolerated cleanup failure must not fail the group")
	assert.Equal(t, []string{"test", "cleanup"}, r.commandSeq())
}

// A tolerated failure must not suppress a later real one.
func TestRunSequence_FailureAfterToleratedOneStillFails(t *testing.T) {
	t.Parallel()
	hard := errors.New("hard failure")
	r, err := runSeq(t, []config.CommandSpec{
		{Command: "lint", ContinueOnError: true},
		{Command: "deploy"},
	}, map[string]error{"lint": errors.New("tolerated"), "deploy": hard})

	require.Error(t, err)
	assert.ErrorIs(t, err, hard)
	assert.Equal(t, []string{"lint", "deploy"}, r.commandSeq())
}

func TestRunSequence_AlwaysRunsAfterCancellation(t *testing.T) {
	t.Parallel()
	r := &specRunner{outputs: map[string]string{}}
	e := New(&config.Config{Version: 2}, WithRunner(r))
	commands := []config.CommandSpec{
		{Command: "work"},
		{Command: "skipped"},
		{Command: "teardown", Always: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := e.runSequence(ctx, &config.Group{Name: "g"}, commands)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled, "cancellation is still reported")
	assert.Equal(t, []string{"teardown"}, r.commandSeq(),
		"only always entries run once the context is dead")
}

func TestRunSequence_CancellationWithNoAlwaysEntriesRunsNothing(t *testing.T) {
	t.Parallel()
	r := &specRunner{outputs: map[string]string{}}
	e := New(&config.Config{Version: 2}, WithRunner(r))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := e.runSequence(ctx, &config.Group{Name: "g"},
		[]config.CommandSpec{{Command: "work"}, {Command: "more"}})
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, r.commandSeq())
}

// cancelOnFailRunner fails one command and cancels the run at the same moment,
// reproducing "the command failed, then the group timed out" deterministically.
type cancelOnFailRunner struct {
	failOn string
	err    error
	cancel context.CancelFunc
	seen   []string
}

func (r *cancelOnFailRunner) Run(
	_ context.Context, g *config.Group, _ config.CommandSpec, _ map[string]string,
) (result.RunResult, error) {
	r.seen = append(r.seen, g.Command)
	if g.Command == r.failOn {
		r.cancel()
		return result.RunResult{Status: result.StatusOK, ExitCode: 1}, r.err
	}
	return result.RunResult{Status: result.StatusOK}, nil
}

// An earlier real failure is more actionable than the cancellation that
// followed it, so it stays the reported error.
func TestRunSequence_EarlierFailureWinsOverCancellation(t *testing.T) {
	t.Parallel()
	boom := errors.New("the real cause")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &cancelOnFailRunner{failOn: "work", err: boom, cancel: cancel}
	e := New(&config.Config{Version: 2}, WithRunner(r))

	_, err := e.runSequence(ctx, &config.Group{Name: "g"}, []config.CommandSpec{
		{Command: "work"},
		{Command: "teardown", Always: true},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
	assert.Equal(t, []string{"work", "teardown"}, r.seen,
		"teardown still runs even though the context died")
}

// Regression guard, not a new behavior: expandCommands copies the spec, so
// non-templated fields must reach the runner untouched.
func TestExpandCommands_CarriesFailurePolicy(t *testing.T) {
	t.Parallel()
	e := New(&config.Config{Version: 2})
	g := &config.Group{Name: "g", Commands: []config.CommandSpec{
		{Command: "a", ContinueOnError: true},
		{Command: "b", Always: true},
	}}

	got, err := e.expandCommands(g, template.Data{})
	require.NoError(t, err)
	assert.True(t, got[0].ContinueOnError)
	assert.True(t, got[1].Always)
}

func TestRunSequence_OutputOfAlwaysEntryIsAggregated(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	r := &specRunner{
		errs:    map[string]error{"test": boom},
		outputs: map[string]string{"test": "testing\n", "teardown": "cleaning\n"},
	}
	e := New(&config.Config{Version: 2}, WithRunner(r))
	commands := []config.CommandSpec{{Command: "test"}, {Command: "teardown", Always: true}}

	out, err := e.runSequence(context.Background(), &config.Group{Name: "g"}, commands)
	require.Error(t, err)
	assert.Contains(t, out.Output, "testing")
	assert.Contains(t, out.Output, "cleaning")
}
