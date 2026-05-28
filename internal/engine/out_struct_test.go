package engine

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
)

// TestEngine_OutStruct_AllStatuses runs the shared structured-outputs fixture
// end-to-end against the engine and asserts every group's stored RunResult
// carries the expected Status. It also confirms the event stream emits
// flow.end with Status "ok" — a skip cascade is not a failure.
func TestEngine_OutStruct_AllStatuses(t *testing.T) {
	t.Parallel()
	cfg, err := config.LoadConfig("../config/test-resources/config-out-struct.yml")
	require.NoError(t, err)

	r := &fakeRunner{outputs: map[string]string{"build": "built"}}
	var buf bytes.Buffer
	e := New(cfg, WithRunner(r), WithEmitter(NewJSONEmitter(&buf)))
	require.NoError(t, e.RunFlow(context.Background(), "ci"))

	store := e.Outputs()

	buildRR, ok := store.Get("build")
	require.True(t, ok, "build must be stored")
	assert.Equal(t, "ok", buildRR.Status, "build ran successfully")
	assert.Equal(t, "built", buildRR.Output, "build captured its stdout")

	lintRR, ok := store.Get("lint")
	require.True(t, ok, "directly when:-skipped lint must be stored")
	assert.Equal(t, "skipped", lintRR.Status)
	assert.Empty(t, lintRR.Output, "skipped group has no captured output")

	reportRR, ok := store.Get("report")
	require.True(t, ok, "cascade-skipped report must be stored")
	assert.Equal(t, "skipped", reportRR.Status)

	// Event stream confirms the flow ends ok — a skip cascade is not a failure.
	evs := decodeEvents(t, buf.Bytes())
	var flowEnd Event
	for i := range evs {
		if evs[i].Event == EventFlowEnd {
			flowEnd = evs[i]
		}
	}
	require.Equal(t, EventFlowEnd, flowEnd.Event, "flow.end must be emitted")
	assert.Equal(t, StatusOK, flowEnd.Status, "skip cascade must not turn flow.end into a failure")
}
