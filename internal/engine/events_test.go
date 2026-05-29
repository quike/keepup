package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
)

func decodeEvents(t *testing.T, b []byte) []Event {
	t.Helper()
	var evs []Event
	for line := range strings.SplitSeq(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		var e Event
		require.NoError(t, json.Unmarshal([]byte(line), &e))
		evs = append(evs, e)
	}
	return evs
}

// statusOf returns the end status emitted for a group.
func statusOf(evs []Event, group string) string {
	for i := range evs {
		if evs[i].Event == EventGroupEnd && evs[i].Group == group {
			return evs[i].Status
		}
	}
	return ""
}

// reasonOf returns the end reason emitted for a group.
func reasonOf(evs []Event, group string) string {
	for i := range evs {
		if evs[i].Event == EventGroupEnd && evs[i].Group == group {
			return evs[i].Reason
		}
	}
	return ""
}

func TestJSONEmitter_FlowAndGroupEvents(t *testing.T) {
	t.Parallel()
	cfg := stepFlowCfg(t, []config.Group{{Name: "a", Command: "echo"}}, [][]string{{"a"}})
	var buf bytes.Buffer
	e := New(cfg, WithRunner(&fakeRunner{outputs: map[string]string{"a": "x"}}), WithEmitter(NewJSONEmitter(&buf)))
	require.NoError(t, e.RunFlow(context.Background(), "f"))

	evs := decodeEvents(t, buf.Bytes())
	// Expect flow.start, group.start, group.end, flow.end in order.
	require.Len(t, evs, 4)
	assert.Equal(t, EventFlowStart, evs[0].Event)
	assert.Equal(t, "f", evs[0].Flow)
	assert.Equal(t, EventGroupStart, evs[1].Event)
	assert.Equal(t, EventGroupEnd, evs[2].Event)
	assert.Equal(t, StatusOK, evs[2].Status)
	assert.Equal(t, EventFlowEnd, evs[3].Event)
	assert.Equal(t, StatusOK, evs[3].Status)
}

func TestJSONEmitter_Statuses(t *testing.T) {
	t.Parallel()

	t.Run("failed", func(t *testing.T) {
		cfg := stepFlowCfg(t, []config.Group{{Name: "a", Command: "false"}}, [][]string{{"a"}})
		var buf bytes.Buffer
		e := New(cfg, WithRunner(&fakeRunner{errs: map[string]error{"a": errors.New("boom")}}), WithEmitter(NewJSONEmitter(&buf)))
		_ = e.RunFlow(context.Background(), "f")
		evs := decodeEvents(t, buf.Bytes())
		assert.Equal(t, StatusFailed, statusOf(evs, "a"))
		// flow.end is failed too.
		assert.Equal(t, StatusFailed, evs[len(evs)-1].Status)
	})

	t.Run("skipped via skip-if", func(t *testing.T) {
		cfg := stepFlowCfg(t, []config.Group{{Name: "a", Command: "echo", SkipIf: "done"}}, [][]string{{"a"}})
		var buf bytes.Buffer
		p := &scriptedProber{results: map[string]error{"done": nil}}
		e := New(cfg, WithRunner(&fakeRunner{}), WithProber(p), WithEmitter(NewJSONEmitter(&buf)))
		require.NoError(t, e.RunFlow(context.Background(), "f"))
		assert.Equal(t, StatusSkipped, statusOf(decodeEvents(t, buf.Bytes()), "a"))
	})

	t.Run("dry-run", func(t *testing.T) {
		cfg := stepFlowCfg(t, []config.Group{{Name: "a", Command: "echo"}}, [][]string{{"a"}})
		var buf bytes.Buffer
		e := New(cfg, WithRunner(&fakeRunner{}), WithDryRun(true), WithEmitter(NewJSONEmitter(&buf)))
		require.NoError(t, e.RunFlow(context.Background(), "f"))
		assert.Equal(t, StatusDryRun, statusOf(decodeEvents(t, buf.Bytes()), "a"))
	})
}

func TestNopEmitterIsDefault(t *testing.T) {
	t.Parallel()
	// No emitter configured → no panic, runs fine.
	cfg := stepFlowCfg(t, []config.Group{{Name: "a", Command: "echo"}}, [][]string{{"a"}})
	e := New(cfg, WithRunner(&fakeRunner{}))
	require.NoError(t, e.RunFlow(context.Background(), "f"))
}

func TestJSONEmitter_DAGSkippedReason(t *testing.T) {
	t.Parallel()
	cfg, err := config.LoadConfig("../config/test-resources/config-dag-when.yml")
	require.NoError(t, err)

	// test outputs "fail" -> deploy.when false -> deploy skipped (reason "when")
	// -> report cascade-skipped (reason mentions upstream deploy).
	var buf bytes.Buffer
	r := &fakeRunner{outputs: map[string]string{"build": "built", "test": "fail"}}
	e := New(cfg, WithRunner(r), WithEmitter(NewJSONEmitter(&buf)))
	require.NoError(t, e.RunFlow(context.Background(), "ci"))

	evs := decodeEvents(t, buf.Bytes())
	assert.Equal(t, StatusSkipped, statusOf(evs, "deploy"))
	assert.Equal(t, StatusSkipped, statusOf(evs, "report"))
	assert.Equal(t, "when", reasonOf(evs, "deploy"))
	assert.Contains(t, reasonOf(evs, "report"), "deploy")

	// Gap #8 from self-review: a skip cascade is NOT a failure — the flow
	// itself must still end "ok" so CI tooling doesn't mistake a conditional
	// branch for a broken run.
	var flowEnd Event
	for i := range evs {
		if evs[i].Event == EventFlowEnd {
			flowEnd = evs[i]
		}
	}
	assert.Equal(t, EventFlowEnd, flowEnd.Event, "flow.end event must be emitted")
	assert.Equal(t, StatusOK, flowEnd.Status, "skip cascade must not turn flow.end into a failure")
}

// TestEvent_WatchTriggerJSONRoundTrip pins the wire format for the new
// watch.trigger event: the Files field must survive a JSON round-trip and
// preserve order (we emit files sorted at the watch layer).
func TestEvent_WatchTriggerJSONRoundTrip(t *testing.T) {
	t.Parallel()
	in := Event{
		Event: EventWatchTrigger,
		Files: []string{"a.go", "b.go", "c.go"},
	}
	b, err := json.Marshal(&in)
	require.NoError(t, err)

	var out Event
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, in.Event, out.Event)
	assert.Equal(t, in.Files, out.Files)

	// Wire-format assertion: the JSON contains a non-empty files array.
	got := string(b)
	assert.Contains(t, got, `"event":"watch.trigger"`)
	assert.Contains(t, got, `"files":["a.go","b.go","c.go"]`)
}

// TestEvent_FilesOmitEmpty asserts non-watch events do not include a "files"
// key in their JSON — preserving byte-identical output for all existing
// consumers of the event stream.
func TestEvent_FilesOmitEmpty(t *testing.T) {
	t.Parallel()
	in := Event{Event: EventFlowStart, Flow: "ci"}
	b, err := json.Marshal(&in)
	require.NoError(t, err)
	assert.NotContains(t, string(b), `"files"`,
		"flow.start (and every other non-watch event) must omit files from JSON")
}
