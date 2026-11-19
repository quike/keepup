package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeStdioConfig writes a flow that runs a group printing to stdout, then a
// when:-skipped step, so one run exercises child output, keepup logs, and skip
// events together. The cache lives in a per-test dir so no run is a cache hit
// and nothing lands in the package directory.
func writeStdioConfig(t *testing.T) string {
	t.Helper()
	return writeTempConfig(t, fmt.Sprintf(stdioCfg, t.TempDir()))
}

const stdioCfg = `
version: 2
settings:
  cache-dir: %q
groups:
  - name: hello
    command: echo
    params: ["child-says-hello"]
    cache:
      reads: ["*.txt"]
  - name: never
    command: echo
    params: ["never"]
default: f
flows:
  f:
    mode: step
    steps:
      - run: [hello]
      - run: [never]
        when: "false"
`

func TestChildStdout(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	tests := []struct {
		name       string
		eventsPath string
		want       *bytes.Buffer
	}{
		{name: "no events", eventsPath: "", want: &stdout},
		{name: "events to file", eventsPath: "run.jsonl", want: &stdout},
		{name: "events to stdout", eventsPath: eventsToStdout, want: &stderr},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Same(t, tc.want, childStdout(tc.eventsPath, &stdout, &stderr))
		})
	}
}

// requireNDJSON fails unless every non-empty line of s is a JSON object.
func requireNDJSON(t *testing.T, s string) {
	t.Helper()
	require.NotEmpty(t, strings.TrimSpace(s), "expected an event stream on stdout")
	for line := range strings.SplitSeq(strings.TrimSpace(s), "\n") {
		var obj map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &obj), "stdout line is not JSON: %q", line)
	}
}

// TestRunCmd_EventsStdoutIsPureJSON is the #72 acceptance test: with
// --events -, stdout carries only NDJSON; keepup logs, the --verbose config
// dump, and child-process output all go to stderr.
func TestRunCmd_EventsStdoutIsPureJSON(t *testing.T) {
	t.Parallel()
	cfgPath := writeStdioConfig(t)
	var stdout, stderr bytes.Buffer
	cmd := newRootCmd(&stdout, &stderr)
	cmd.SetArgs([]string{"run", "f", "--config", cfgPath, "--verbose", "--events", "-"})
	require.NoError(t, cmd.Execute())

	requireNDJSON(t, stdout.String())
	assert.Contains(t, stdout.String(), `"group":"never","status":"skipped","reason":"when"`)
	assert.Contains(t, stderr.String(), "child-says-hello", "child stdout must move to stderr")
	assert.Contains(t, stderr.String(), "starting flow", "keepup logs must go to stderr")
	assert.Contains(t, stderr.String(), "# config:", "the verbose dump must go to stderr")
}

// TestRunCmd_ChildStdoutStaysOnStdout pins the default: without --events -,
// a command's stdout is still the CLI's stdout (pipeable), while keepup's own
// logs go to stderr.
func TestRunCmd_ChildStdoutStaysOnStdout(t *testing.T) {
	t.Parallel()
	cfgPath := writeStdioConfig(t)
	var stdout, stderr bytes.Buffer
	cmd := newRootCmd(&stdout, &stderr)
	cmd.SetArgs([]string{"run", "f", "--config", cfgPath})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, "child-says-hello\n", stdout.String(), "stdout carries only the child's output")
	assert.Contains(t, stderr.String(), "starting flow")
}

// TestWatchCmd_EventsStdoutIsPureJSON drives the real watch command through
// its initial run and asserts the same stdout purity as run.
func TestWatchCmd_EventsStdoutIsPureJSON(t *testing.T) {
	t.Parallel()
	cfgPath := writeStdioConfig(t)
	stdout, stderr := &syncBuf{}, &syncBuf{}
	cmd := newRootCmd(stdout, stderr)
	cmd.SetArgs([]string{"watch", "f", "--config", cfgPath, "--events", "-"})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cmd.SetContext(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = cmd.Execute()
	}()

	require.Eventually(t, func() bool {
		return strings.Contains(stdout.String(), `"event":"flow.end"`)
	}, 5*time.Second, 10*time.Millisecond)
	cancel()
	<-done

	requireNDJSON(t, stdout.String())
	assert.Contains(t, stderr.String(), "child-says-hello")
	assert.Contains(t, stderr.String(), "starting flow")
}
