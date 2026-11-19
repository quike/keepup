package engine

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/result"
)

// These tests exercise the real template.Expander through the engine, covering
// both backward-compatible legacy syntax and the new function/sprig forms.

func TestEngine_Template_LegacyAndNewForms(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		params []string
		want   string // expected recorded "consumer:<joined params>"
	}{
		{"legacy dotted form", []string{"{{ output.producer }}"}, "consumer:banana"},
		{"new function form", []string{`{{ output "producer" }}`}, "consumer:banana"},
		{"sprig pipe", []string{`{{ output "producer" | upper }}`}, "consumer:BANANA"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := stepFlowCfg(t, []config.Group{
				{Name: "producer", Command: "echo", Params: []string{"banana"}},
				{Name: "consumer", Command: "echo", Params: tc.params},
			}, [][]string{{"producer"}, {"consumer"}})
			r := &fakeRunner{outputs: map[string]string{"producer": "banana\n"}}
			e := New(cfg, WithRunner(r))

			require.NoError(t, e.RunFlow(context.Background(), "f"))
			require.Len(t, r.calls, 2)
			assert.Equal(t, tc.want, r.calls[1])
		})
	}
}

func TestEngine_Template_EnvFunc(t *testing.T) {
	t.Parallel()
	cfg := stepFlowCfg(t, []config.Group{
		{Name: "a", Command: "echo", Params: []string{`{{ env "GREETING" }}`}},
	}, [][]string{{"a"}})
	cfg.Env = map[string]string{"GREETING": "hi"}
	r := &fakeRunner{outputs: map[string]string{"a": ""}}
	e := New(cfg, WithRunner(r))

	require.NoError(t, e.RunFlow(context.Background(), "f"))
	assert.Equal(t, "a:hi", r.calls[0])
}

// TestEngine_Template_EnvLayering pins that template `env` sees the same
// layering the command's runtime environment gets: process < global < group
// < command (for the command's own command/params/dir).
func TestEngine_Template_EnvLayering(t *testing.T) {
	t.Parallel()
	const tmpl = `{{ env "LAYER" }}`
	tests := []struct {
		name     string
		global   map[string]string
		groupEnv map[string]string
		cmdEnv   map[string]string
		want     string
	}{
		{name: "process env is visible", want: "a:process"},
		{name: "global overrides process", global: map[string]string{"LAYER": "global"}, want: "a:global"},
		{
			name:     "group overrides global",
			global:   map[string]string{"LAYER": "global"},
			groupEnv: map[string]string{"LAYER": "group"},
			want:     "a:group",
		},
		{
			name:     "command overrides group",
			global:   map[string]string{"LAYER": "global"},
			groupEnv: map[string]string{"LAYER": "group"},
			cmdEnv:   map[string]string{"LAYER": "command"},
			want:     "a:command",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := stepFlowCfg(t, []config.Group{{
				Name:     "a",
				Env:      tc.groupEnv,
				Commands: []config.CommandSpec{{Command: "echo", Params: []string{tmpl}, Env: tc.cmdEnv}},
			}}, [][]string{{"a"}})
			cfg.Env = tc.global
			r := &fakeRunner{}
			e := New(cfg, WithRunner(r), WithProcessEnv([]string{"LAYER=process"}))

			require.NoError(t, e.RunFlow(context.Background(), "f"))
			assert.Equal(t, []string{tc.want}, r.calls)
		})
	}
}

// TestEngine_Template_CommandEnvRendersAgainstGroupLayers pins that a
// command's own env: values are rendered against process < global < group
// (they cannot see themselves), and the rendered values then feed that
// command's params.
func TestEngine_Template_CommandEnvRendersAgainstGroupLayers(t *testing.T) {
	t.Parallel()
	cfg := stepFlowCfg(t, []config.Group{{
		Name: "a",
		Env:  map[string]string{"BASE": "group"},
		Commands: []config.CommandSpec{{
			Command: "echo",
			Params:  []string{`{{ env "DERIVED" }}`},
			Env:     map[string]string{"DERIVED": `{{ env "BASE" }}-{{ env "HOME_ISH" }}`},
		}},
	}}, [][]string{{"a"}})
	r := &fakeRunner{}
	e := New(cfg, WithRunner(r), WithProcessEnv([]string{"HOME_ISH=proc"}))

	require.NoError(t, e.RunFlow(context.Background(), "f"))
	assert.Equal(t, []string{"a:group-proc"}, r.calls)
}

// TestEngine_Template_EnvIgnoresUninjectedProcessEnv pins that the template
// environment comes from the injected process env, not os.Environ, so tests
// stay hermetic.
func TestEngine_Template_EnvIgnoresUninjectedProcessEnv(t *testing.T) {
	t.Setenv("KEEPUP_TEST_OS_ONLY", "leaked")
	cfg := stepFlowCfg(t, []config.Group{
		{Name: "a", Command: "echo", Params: []string{`{{ env "KEEPUP_TEST_OS_ONLY" }}`}},
	}, [][]string{{"a"}})
	r := &fakeRunner{}
	e := New(cfg, WithRunner(r), WithProcessEnv(nil))

	require.NoError(t, e.RunFlow(context.Background(), "f"))
	assert.Equal(t, []string{"a:"}, r.calls)
}

func TestEngine_Template_CommandIsExpanded(t *testing.T) {
	t.Parallel()
	// The command itself (not just params) is template-expanded.
	cfg := stepFlowCfg(t, []config.Group{
		{Name: "producer", Command: "echo", Params: []string{"world"}},
		{Name: "consumer", Command: `echo-{{ output "producer" }}`},
	}, [][]string{{"producer"}, {"consumer"}})
	r := &captureCommandRunner{}
	e := New(cfg, WithRunner(r))

	require.NoError(t, e.RunFlow(context.Background(), "f"))
	assert.Equal(t, "echo-world", r.lastCommand)
}

func TestEngine_Template_BadTemplateFailsGroup(t *testing.T) {
	t.Parallel()
	cfg := stepFlowCfg(t, []config.Group{
		{Name: "a", Command: "echo", Params: []string{`{{ bogusfunc "x" }}`}},
	}, [][]string{{"a"}})
	e := New(cfg, WithRunner(&fakeRunner{}))
	err := e.RunFlow(context.Background(), "f")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expand param")
}

func TestEngine_Template_FromFixture(t *testing.T) {
	t.Parallel()
	cfg, err := config.LoadConfig("../config/test-resources/config-template.yml")
	require.NoError(t, err)

	r := &recordingRunner{outputs: map[string]string{"producer": "abcdef1234567890\n"}}
	e := New(cfg, WithRunner(r))
	require.NoError(t, e.RunFlow(context.Background(), "pipeline"))

	assert.Equal(t, "sha=abcdef1234567890", r.params["legacy-consumer"])
	assert.Equal(t, "sha=abcdef1234567890", r.params["func-consumer"])
	assert.Equal(t, "short=abcdef1", r.params["sprig-consumer"])
	assert.Equal(t, "ci=true", r.params["env-consumer"])
	// cmd-consumer templates its command, not its params.
	assert.Equal(t, "echo-abc", r.commands["cmd-consumer"])
}

// recordingRunner records the expanded command and joined params per group.
type recordingRunner struct {
	mu       sync.Mutex
	outputs  map[string]string
	params   map[string]string
	commands map[string]string
}

func (r *recordingRunner) Run(_ context.Context, g *config.Group, spec config.CommandSpec, _ map[string]string) (result.RunResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.params == nil {
		r.params = map[string]string{}
		r.commands = map[string]string{}
	}
	r.params[g.Name] = strings.Join(spec.Params, ",")
	r.commands[g.Name] = g.Command
	stdout := r.outputs[g.Name]
	return result.RunResult{Stdout: stdout, Output: stdout, Status: "ok"}, nil
}

func TestEngine_Template_BadCommandFailsGroup(t *testing.T) {
	t.Parallel()
	cfg := stepFlowCfg(t, []config.Group{
		{Name: "a", Command: `{{ bogusfunc }}`},
	}, [][]string{{"a"}})
	e := New(cfg, WithRunner(&fakeRunner{}))
	err := e.RunFlow(context.Background(), "f")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expand command")
}

// captureCommandRunner records the (expanded) command of the last group run
// and echoes its first param as output so downstream refs resolve.
type captureCommandRunner struct{ lastCommand string }

func (r *captureCommandRunner) Run(
	_ context.Context, g *config.Group, spec config.CommandSpec, _ map[string]string,
) (result.RunResult, error) {
	r.lastCommand = g.Command
	if len(spec.Params) > 0 {
		return result.RunResult{Stdout: spec.Params[0], Output: spec.Params[0], Status: "ok"}, nil
	}
	return result.RunResult{Status: "ok"}, nil
}
