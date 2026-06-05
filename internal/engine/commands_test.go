package engine

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/result"
	"github.com/quike/keepup/internal/template"
)

// specRunner records each command invocation (argv0, params, shell) and
// returns scripted results keyed by argv0. failOnce entries fail the first
// invocation of that command and succeed afterwards.
type specRunner struct {
	mu       sync.Mutex
	calls    []specCall
	outputs  map[string]string
	errs     map[string]error
	failOnce map[string]error
}

type specCall struct {
	command string
	params  []string
	shell   string
}

func (f *specRunner) Run(_ context.Context, g *config.Group, params []string, _ map[string]string) (result.RunResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, specCall{command: g.Command, params: append([]string(nil), params...), shell: g.Shell})
	out := f.outputs[g.Command]
	rr := result.RunResult{Stdout: out, Output: out, Status: result.StatusOK, DurationMs: 1}
	if err, ok := f.failOnce[g.Command]; ok {
		delete(f.failOnce, g.Command)
		rr.ExitCode = 1
		return rr, err
	}
	if err, ok := f.errs[g.Command]; ok {
		rr.ExitCode = 1
		return rr, err
	}
	return rr, nil
}

func (f *specRunner) commandSeq() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	seq := make([]string, len(f.calls))
	for i, c := range f.calls {
		seq[i] = c.command
	}
	return seq
}

// multiGroup is the canonical mixed-form group used across these tests.
func multiGroup() config.Group {
	return config.Group{
		Name:  "m",
		Shell: "fakesh",
		Commands: []config.CommandSpec{
			{Command: "alpha", Params: []string{"-a"}},         // argv form
			{Command: "beta line", IsShell: true},              // shell string form
			{Command: "gamma one\ngamma two\n", IsShell: true}, // script form
		},
	}
}

func TestEngine_MultiCommand_RunsInOrderAndCombines(t *testing.T) {
	cfg := stepFlowCfg(t, []config.Group{multiGroup()}, [][]string{{"m"}})
	r := &specRunner{outputs: map[string]string{
		"alpha":                  "A\n",
		"beta line":              "B\n",
		"gamma one\ngamma two\n": "C\n",
	}}
	e := New(cfg, WithRunner(r))

	require.NoError(t, e.RunFlow(context.Background(), "f"))

	assert.Equal(t, []string{"alpha", "beta line", "gamma one\ngamma two\n"}, r.commandSeq())
	got, ok := e.Outputs().Get("m")
	require.True(t, ok)
	assert.Equal(t, "A\nB\nC\n", got.Output)
	assert.Equal(t, "A\nB\nC\n", got.Stdout)
	assert.Equal(t, 0, got.ExitCode)
	assert.Equal(t, int64(3), got.DurationMs, "duration sums across the sequence")
	assert.Equal(t, result.StatusOK, got.Status)
}

func TestEngine_MultiCommand_ArgvEntryIgnoresShell(t *testing.T) {
	cfg := stepFlowCfg(t, []config.Group{multiGroup()}, [][]string{{"m"}})
	r := &specRunner{outputs: map[string]string{}}
	e := New(cfg, WithRunner(r))

	require.NoError(t, e.RunFlow(context.Background(), "f"))

	require.Len(t, r.calls, 3)
	assert.Equal(t, "", r.calls[0].shell, "argv entry must clear shell even when group sets it")
	assert.Equal(t, []string{"-a"}, r.calls[0].params)
	assert.Equal(t, "fakesh", r.calls[1].shell, "string entry keeps the group shell")
	assert.Empty(t, r.calls[1].params)
	assert.Equal(t, "fakesh", r.calls[2].shell, "script entry keeps the group shell")
}

func TestEngine_MultiCommand_StopsOnFirstFailure(t *testing.T) {
	cfg := stepFlowCfg(t, []config.Group{multiGroup()}, [][]string{{"m"}})
	r := &specRunner{
		outputs: map[string]string{"alpha": "A\n"},
		errs:    map[string]error{"beta line": assert.AnError},
	}
	e := New(cfg, WithRunner(r))

	err := e.RunFlow(context.Background(), "f")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command 2 of 3")
	assert.Equal(t, []string{"alpha", "beta line"}, r.commandSeq(), "third command must not run")
	_, ok := e.Outputs().Get("m")
	assert.False(t, ok, "failed group stores no output")
}

func TestRunSequence_AggregatesExitCodeAndOutput(t *testing.T) {
	r := &specRunner{
		outputs: map[string]string{"alpha": "A\n"},
		errs:    map[string]error{"beta line": assert.AnError},
	}
	g := multiGroup()
	cfg := stepFlowCfg(t, []config.Group{g}, [][]string{{"m"}})
	e := New(cfg, WithRunner(r))

	specs := g.CommandList()
	out, err := e.runSequence(context.Background(), &g, specs)
	require.Error(t, err)
	assert.Equal(t, "A\n", out.Output, "combined output covers commands that ran")
	assert.Equal(t, 1, out.ExitCode, "exit code is the first failing command's")
	assert.Equal(t, int64(2), out.DurationMs)
}

func TestEngine_MultiCommand_RetryReplaysWholeSequence(t *testing.T) {
	cfg := stepFlowCfg(t, []config.Group{multiGroup()}, [][]string{{"m"}})
	cfg.Flows["f"] = config.Flow{
		Mode:    config.ModeStep,
		Steps:   []config.Step{{Run: []string{"m"}}},
		Retries: 1,
	}
	r := &specRunner{
		outputs:  map[string]string{},
		failOnce: map[string]error{"beta line": assert.AnError},
	}
	e := New(cfg, WithRunner(r), WithRetryBackoff(0))

	require.NoError(t, e.RunFlow(context.Background(), "f"))
	assert.Equal(t,
		[]string{"alpha", "beta line", "alpha", "beta line", "gamma one\ngamma two\n"},
		r.commandSeq(), "retry restarts from the first command")
}

func TestEngine_MultiCommand_DryRunRunsNothing(t *testing.T) {
	cfg := stepFlowCfg(t, []config.Group{multiGroup()}, [][]string{{"m"}})
	r := &specRunner{}
	e := New(cfg, WithRunner(r), WithDryRun(true))

	require.NoError(t, e.RunFlow(context.Background(), "f"))
	assert.Empty(t, r.calls)
	got, ok := e.Outputs().Get("m")
	require.True(t, ok)
	assert.Equal(t, result.StatusDryRun, got.Status)
}

func TestEngine_MultiCommand_TemplatesExpandPerEntry(t *testing.T) {
	groups := []config.Group{
		{Name: "a", Command: "echo"},
		{Name: "m2", Shell: "fakesh", Commands: []config.CommandSpec{
			{Command: "echo", Params: []string{`{{ output "a" }}`}},
			{Command: `use {{ output "a" }}`, IsShell: true},
		}},
	}
	cfg := stepFlowCfg(t, groups, [][]string{{"a"}, {"m2"}})
	r := &specRunner{outputs: map[string]string{"echo": "VAL\n"}}
	e := New(cfg, WithRunner(r), WithExpander(template.NewExpander()))

	require.NoError(t, e.RunFlow(context.Background(), "f"))
	require.Len(t, r.calls, 3)
	assert.Equal(t, []string{"VAL"}, r.calls[1].params, "argv param expanded")
	assert.Equal(t, "use VAL", r.calls[2].command, "shell string expanded")
}

func TestEngine_MultiCommand_RealRunner(t *testing.T) {
	skipOnWindows(t)

	run := func(t *testing.T, g config.Group) (result.RunResult, error) {
		t.Helper()
		cfg := stepFlowCfg(t, []config.Group{g}, [][]string{{g.Name}})
		e := New(cfg, WithRunner(&ShellRunner{Stdout: io.Discard, Stderr: io.Discard}))
		err := e.RunFlow(context.Background(), "f")
		out, _ := e.Outputs().Get(g.Name)
		return out, err
	}

	t.Run("argv entry stays safe-exec even with shell set", func(t *testing.T) {
		out, err := run(t, config.Group{Name: "g", Shell: "sh", Commands: []config.CommandSpec{
			{Command: "echo", Params: []string{"$(whoami)", "*"}},
		}})
		require.NoError(t, err)
		assert.Equal(t, "$(whoami) *\n", out.Output, "no shell interpretation, no glob expansion")
	})

	t.Run("bare string runs through the declared shell", func(t *testing.T) {
		out, err := run(t, config.Group{Name: "g", Shell: "sh", Commands: []config.CommandSpec{
			{Command: "printf 'hi' | tr h H", IsShell: true},
		}})
		require.NoError(t, err)
		assert.Equal(t, "Hi", out.Output, "pipes prove shell interpretation")
	})

	t.Run("multiline script runs as one script", func(t *testing.T) {
		out, err := run(t, config.Group{Name: "g", Shell: "sh", Commands: []config.CommandSpec{
			{Command: "X=fromscript\necho $X\n", IsShell: true},
		}})
		require.NoError(t, err)
		assert.Equal(t, "fromscript\n", out.Output, "variable spans lines: one script, not line-by-line")
	})

	t.Run("mixed forms interleave in order", func(t *testing.T) {
		out, err := run(t, config.Group{Name: "g", Shell: "sh", Commands: []config.CommandSpec{
			{Command: "echo", Params: []string{"one"}},
			{Command: "echo two", IsShell: true},
			{Command: "echo", Params: []string{"three"}},
		}})
		require.NoError(t, err)
		assert.Equal(t, "one\ntwo\nthree\n", out.Output)
	})

	t.Run("first non-zero exit stops the sequence", func(t *testing.T) {
		g := config.Group{Name: "g", Shell: "sh", Commands: []config.CommandSpec{
			{Command: "echo", Params: []string{"ran"}},
			{Command: "exit 3", IsShell: true},
			{Command: "echo", Params: []string{"never"}},
		}}
		cfg := stepFlowCfg(t, []config.Group{g}, [][]string{{"g"}})
		e := New(cfg, WithRunner(&ShellRunner{Stdout: io.Discard, Stderr: io.Discard}))

		out, err := e.runSequence(context.Background(), &g, g.CommandList())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "command 2 of 3")
		assert.Equal(t, "ran\n", out.Output)
		assert.Equal(t, 3, out.ExitCode, "exit code is the first failing command's")
		assert.NotContains(t, out.Output, "never")
	})
}
