package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
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

// captureCommandRunner records the (expanded) command of the last group run
// and echoes its first param as output so downstream refs resolve.
type captureCommandRunner struct{ lastCommand string }

func (r *captureCommandRunner) Run(_ context.Context, g *config.Group, params []string, _ map[string]string) (string, error) {
	r.lastCommand = g.Command
	if len(params) > 0 {
		return params[0], nil
	}
	return "", nil
}
