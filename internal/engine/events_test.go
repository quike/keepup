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
}
