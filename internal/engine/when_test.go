package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
)

// stepFlowWithWhen builds a one-step flow whose single group is "a", with the
// given `when` predicate on that step.
func stepFlowWithWhen(when string) *config.Config {
	return &config.Config{
		Version: config.SchemaVersion,
		Groups:  []config.Group{{Name: "a", Command: "echo"}},
		Flows: map[string]config.Flow{
			"f": {Mode: config.ModeStep, Steps: []config.Step{{Run: []string{"a"}, When: when}}},
		},
	}
}

func TestEngine_When(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		when    string
		wantRun bool
	}{
		{"literal true runs", "true", true},
		{"literal false skips", "false", false},
		{"empty render skips", `{{ env "MISSING" }}`, false},
		{"zero skips", "0", false},
		{"no/off skip", "off", false},
		{"non-empty text runs", "yes", true},
		{"sprig predicate true", `{{ eq "x" "x" }}`, true},
		{"sprig predicate false", `{{ eq "x" "y" }}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &fakeRunner{outputs: map[string]string{"a": "ran"}}
			e := New(stepFlowWithWhen(tc.when), WithRunner(r))
			require.NoError(t, e.RunFlow(context.Background(), "f"))
			if tc.wantRun {
				assert.Equal(t, []string{"a:"}, r.calls)
			} else {
				assert.Empty(t, r.calls, "step should have been skipped")
			}
		})
	}
}

func TestEngine_When_ReferencesEarlierOutput(t *testing.T) {
	t.Parallel()
	// gate runs only if the producer's output contains "GO".
	cfg := &config.Config{
		Version: config.SchemaVersion,
		Groups: []config.Group{
			{Name: "producer", Command: "echo"},
			{Name: "gated", Command: "echo"},
		},
		Flows: map[string]config.Flow{
			"f": {Mode: config.ModeStep, Steps: []config.Step{
				{Run: []string{"producer"}},
				{Run: []string{"gated"}, When: `{{ contains "GO" (output "producer") }}`},
			}},
		},
	}
	r := &fakeRunner{outputs: map[string]string{"producer": "GO\n", "gated": "done"}}
	e := New(cfg, WithRunner(r))
	require.NoError(t, e.RunFlow(context.Background(), "f"))
	assert.Equal(t, []string{"producer:", "gated:"}, r.calls)
}

func TestEngine_When_FromFixture(t *testing.T) {
	t.Parallel()
	cfg, err := config.LoadConfig("../config/test-resources/config-when.yml")
	require.NoError(t, err)

	r := &fakeRunner{outputs: map[string]string{"tests": "PASS\n"}}
	e := New(cfg, WithRunner(r))
	require.NoError(t, e.RunFlow(context.Background(), "ci"))

	// build runs (tests PASS), deploy runs (DEPLOY=true), rollback is skipped
	// (FORCE_ROLLBACK unset), notify always runs.
	assert.Equal(t, []string{"tests:PASS", "build:built", "deploy:deploying", "notify:notifying"}, r.calls)
}

func TestEngine_When_EdgesFlow(t *testing.T) {
	t.Parallel()
	cfg, err := config.LoadConfig("../config/test-resources/config-when.yml")
	require.NoError(t, err)

	r := &fakeRunner{}
	e := New(cfg, WithRunner(r))
	require.NoError(t, e.RunFlow(context.Background(), "edges"))

	// and/or/not/num all evaluate truthy; e-skip is gated false.
	assert.Equal(t, []string{"e-and:and", "e-or:or", "e-not:not", "e-num:num"}, r.calls)
}

func TestEngine_When_BadTemplateErrors(t *testing.T) {
	t.Parallel()
	e := New(stepFlowWithWhen(`{{ fail "boom" }}`), WithRunner(&fakeRunner{}))
	err := e.RunFlow(context.Background(), "f")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "when")
}

// TestStep_SkippedStepStoresEachGroupAsSkipped covers the step-mode analog:
// a when:-skipped step now stores RunResult{Status:"skipped"} for each of its
// groups, so (out "x").Status reads "skipped" instead of "" (which would mean
// "never declared").
func TestStep_SkippedStepStoresEachGroupAsSkipped(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Version: config.SchemaVersion,
		Groups: []config.Group{
			{Name: "a", Command: "echo"},
			{Name: "b", Command: "echo"},
		},
		Flows: map[string]config.Flow{
			"f": {Mode: config.ModeStep, Steps: []config.Step{
				{Run: []string{"a"}, When: "false"}, // step skipped
				{Run: []string{"b"}},                // runs normally
			}},
		},
	}
	r := &fakeRunner{outputs: map[string]string{"b": "ran"}}
	e := New(cfg, WithRunner(r))
	require.NoError(t, e.RunFlow(context.Background(), "f"))

	store := e.Outputs()

	aRR, ok := store.Get("a")
	require.True(t, ok, "step-skipped group must be stored")
	assert.Equal(t, "skipped", aRR.Status)

	// 'a' was skipped; 'b' should be the only call.
	assert.Equal(t, []string{"b:"}, r.calls)

	bRR, ok := store.Get("b")
	require.True(t, ok)
	assert.Equal(t, "ok", bRR.Status)
}
