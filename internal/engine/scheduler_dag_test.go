package engine

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
)

// ranNames returns the set of group names the runner was invoked for.
func ranNames(r *fakeRunner) map[string]bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]bool, len(r.calls))
	for _, c := range r.calls {
		name := c
		if i := strings.IndexByte(c, ':'); i >= 0 {
			name = c[:i]
		}
		out[name] = true
	}
	return out
}

func TestDAG_When_SkipsGroupAndCascades(t *testing.T) {
	cfg, err := config.LoadConfig("../config/test-resources/config-dag-when.yml")
	require.NoError(t, err)

	// test outputs "fail" -> deploy.when (eq output(test) "pass") is false ->
	// deploy is skipped -> report (consumes deploy's output) cascade-skips.
	r := &fakeRunner{outputs: map[string]string{
		"build": "built", "test": "fail",
	}}
	e := New(cfg, WithRunner(r))
	require.NoError(t, e.RunFlow(context.Background(), "ci"))

	ran := ranNames(r)
	assert.True(t, ran["build"], "build should run")
	assert.True(t, ran["test"], "test should run")
	assert.False(t, ran["deploy"], "deploy should be skipped (when false)")
	assert.False(t, ran["report"], "report should cascade-skip (deploy skipped)")
}

func TestDAG_When_RunsWhenTrue(t *testing.T) {
	cfg, err := config.LoadConfig("../config/test-resources/config-dag-when.yml")
	require.NoError(t, err)

	// test outputs "pass" -> deploy runs -> report consumes deploy output.
	r := &fakeRunner{outputs: map[string]string{
		"build": "built", "test": "pass", "deploy": "deploying",
	}}
	e := New(cfg, WithRunner(r))
	require.NoError(t, e.RunFlow(context.Background(), "ci"))

	ran := ranNames(r)
	for _, name := range []string{"build", "test", "deploy", "report"} {
		assert.True(t, ran[name], "%s should run when test passes", name)
	}
}

// TestDAG_When_DryRunStillEvaluatesPredicate covers gap #9 from the
// self-review: in --dry-run, `when:` predicates still evaluate so the dry run
// reveals the real control flow. A when:-skipped group emits status=skipped
// (not dry-run) because the skip decision happens before runGroup is reached;
// a normally-run group is the one that emits dry-run.
func TestDAG_When_DryRunStillEvaluatesPredicate(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Version: config.SchemaVersion,
		Groups: []config.Group{
			{Name: "build", Command: "echo"},
			{Name: "deploy", Command: "echo"},
		},
		Flows: map[string]config.Flow{
			"ci": {Mode: config.ModeDAG, Run: []config.RunEntry{
				{Group: "build"},
				{Group: "deploy", When: "false"},
			}},
		},
	}
	var buf bytes.Buffer
	e := New(cfg, WithRunner(&fakeRunner{}), WithDryRun(true), WithEmitter(NewJSONEmitter(&buf)))
	require.NoError(t, e.RunFlow(context.Background(), "ci"))

	evs := decodeEvents(t, buf.Bytes())
	// build is unconditional → dry-run swallows the actual command but still
	// emits a group.end event with status=dry-run.
	assert.Equal(t, StatusDryRun, statusOf(evs, "build"))
	// deploy's when: rendered falsey → skip beats dry-run.
	assert.Equal(t, StatusSkipped, statusOf(evs, "deploy"))
	assert.Equal(t, "when", reasonOf(evs, "deploy"))
}

// TestDAG_When_RenderErrorAbortsFlow covers gap #7 from the self-review: a
// when: that parses cleanly but errors at render time must abort the flow
// with the wrapped `flow %q: group %q: when: %w` message, cancel in-flight
// work, and prevent dependents from running. Sprig's `fail` reliably returns
// a render-time error.
func TestDAG_When_RenderErrorAbortsFlow(t *testing.T) {
	cfg := &config.Config{
		Version: config.SchemaVersion,
		Groups: []config.Group{
			{Name: "build", Command: "echo"},
			{Name: "deploy", Command: "echo"},
		},
		Flows: map[string]config.Flow{
			"ci": {Mode: config.ModeDAG, Run: []config.RunEntry{
				{Group: "build"},
				{Group: "deploy", When: `{{ fail "boom" }}`},
			}},
		},
	}
	r := &fakeRunner{outputs: map[string]string{"build": "built"}}
	e := New(cfg, WithRunner(r))
	err := e.RunFlow(context.Background(), "ci")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `flow "ci"`)
	assert.Contains(t, err.Error(), `group "deploy"`)
	assert.Contains(t, err.Error(), "when:")
	assert.Contains(t, err.Error(), "boom")
	assert.False(t, ranNames(r)["deploy"], "deploy must not run after its when: errored")
}
