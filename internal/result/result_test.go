package result_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/result"
)

func TestRunResultJSONRoundTrip(t *testing.T) {
	t.Parallel()
	in := result.RunResult{
		Stdout:     "hello\n",
		Stderr:     "warning\n",
		Output:     "hello\nwarning\n",
		ExitCode:   0,
		DurationMs: 42,
		Status:     "ok",
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)

	var out result.RunResult
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, in, out)
}

func TestRunResultZeroValue(t *testing.T) {
	t.Parallel()
	var rr result.RunResult
	assert.Equal(t, "", rr.Status)
	assert.Equal(t, "", rr.Stdout)
	assert.Equal(t, "", rr.Stderr)
	assert.Equal(t, "", rr.Output)
	assert.Equal(t, 0, rr.ExitCode)
	assert.Equal(t, int64(0), rr.DurationMs)
}

func TestRunResultJSONFields(t *testing.T) {
	t.Parallel()
	rr := result.RunResult{Stdout: "s", Stderr: "e", Output: "se", ExitCode: 1, DurationMs: 7, Status: "ok"}
	b, err := json.Marshal(rr)
	require.NoError(t, err)
	got := string(b)
	for _, want := range []string{`"stdout":"s"`, `"stderr":"e"`, `"output":"se"`, `"exitCode":1`, `"durationMs":7`, `"status":"ok"`} {
		assert.Contains(t, got, want)
	}
}
