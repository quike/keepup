package engine

import (
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
