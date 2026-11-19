package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/engine"
)

func TestExitCode(t *testing.T) {
	t.Parallel()
	groupFailed := &engine.GroupFailedError{Group: "g", ExitCode: 3, Err: errors.New("exit status 3")}
	killed := &engine.GroupFailedError{Group: "g", ExitCode: -1, Err: errors.New("signal: killed")}
	notStarted := &engine.GroupFailedError{Group: "g", ExitCode: 0, Err: errors.New("executable not found")}
	timeout := &engine.GroupFailedError{
		Group: "g", ExitCode: -1,
		Err: fmt.Errorf("%w: %w", &engine.TimeoutError{Timeout: time.Second}, errors.New("signal: killed")),
	}
	canceled := &engine.CanceledError{Cause: context.Canceled, Err: timeout}

	tests := []struct {
		name        string
		err         error
		passthrough bool
		want        int
	}{
		{"success", nil, false, 0},
		{"success with passthrough", nil, true, 0},
		{"untyped error", errors.New("boom"), false, 1},
		{"untyped error with passthrough", errors.New("boom"), true, 1},
		{"config error", &config.Error{Err: errors.New("bad yaml")}, false, 2},
		{"wrapped config error", fmt.Errorf("load configuration: %w", &config.Error{Err: errors.New("x")}), false, 2},
		{"usage error", &usageError{err: errors.New("unknown flag")}, false, 2},
		{"group failed", groupFailed, false, 1},
		{"group failed, wrapped", fmt.Errorf("step 1: %w", groupFailed), false, 1},
		{"group failed with passthrough", fmt.Errorf("step 1: %w", groupFailed), true, 3},
		{"killed group with passthrough falls back to 1", killed, true, 1},
		{"unstarted group with passthrough falls back to 1", notStarted, true, 1},
		{"timeout", timeout, false, 124},
		{"timeout wins over passthrough", timeout, true, 124},
		{"canceled", canceled, false, 130},
		{"canceled wins over timeout and passthrough", canceled, true, 130},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, exitCode(tc.err, tc.passthrough))
		})
	}
}

const exitCfg = `
version: 2
groups:
  - name: ok
    command: "true"
  - name: fail
    command: sh
    params: ["-c", "exit 3"]
  - name: slow
    command: sleep
    params: ["5"]
default: pass
flows:
  pass:
    steps:
      - run: [ok]
  fail:
    steps:
      - run: [fail]
  slow:
    timeout: 50ms
    steps:
      - run: [slow]
  hang:
    steps:
      - run: [slow]
`

// TestRun_ExitCodes drives the real CLI entry point end to end, pinning the
// documented exit-code contract.
func TestRun_ExitCodes(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, exitCfg)
	badCfgPath := writeTempConfig(t, "version: 2\ngroups: [unterminated")

	tests := []struct {
		name string
		args []string
		want int
	}{
		{"success", []string{"run", "-c", cfgPath}, 0},
		{"help", []string{"--help"}, 0},
		{"group failure", []string{"run", "fail", "-c", cfgPath}, 1},
		{"group failure with --exit-code", []string{"run", "fail", "--exit-code", "-c", cfgPath}, 3},
		{"timeout", []string{"run", "slow", "-c", cfgPath}, 124},
		{"unknown flow", []string{"run", "ghost", "-c", cfgPath}, 2},
		{"missing config", []string{"run", "-c", cfgPath + ".missing"}, 2},
		{"invalid config", []string{"validate", "-c", badCfgPath}, 2},
		{"unknown flag", []string{"run", "--bogus", "-c", cfgPath}, 2},
		{"too many args", []string{"run", "a", "b", "-c", cfgPath}, 2},
		{"unknown command", []string{"bogus"}, 2},
		{"unknown list target", []string{"list", "bogus", "-c", cfgPath}, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			assert.Equal(t, tc.want, run(context.Background(), tc.args, &out, &out, nil), out.String())
		})
	}
}

func TestRun_CanceledExits130(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, exitCfg)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	var out bytes.Buffer
	assert.Equal(t, 130, run(ctx, []string{"run", "hang", "-c", cfgPath}, &out, &out, nil), out.String())
}

func TestRun_PrintsErrorToStderr(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, exitCfg)
	var stdout, stderr bytes.Buffer
	run(context.Background(), []string{"run", "ghost", "-c", cfgPath}, &stdout, &stderr, nil)
	assert.Contains(t, stderr.String(), `flow "ghost" not found`)
}

// TestRun_PrintsEachErrorOnce pins run() as the single place errors are
// printed, and that usage text accompanies only genuine usage errors.
func TestRun_PrintsEachErrorOnce(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, exitCfg)
	tests := []struct {
		name      string
		args      []string
		message   string
		wantUsage bool
	}{
		{"runtime error", []string{"run", "ghost", "-c", cfgPath}, `flow "ghost" not found`, false},
		{"group failure", []string{"run", "fail", "-c", cfgPath}, "exit status 3", false},
		{"unknown flag", []string{"run", "--bogus", "-c", cfgPath}, "unknown flag: --bogus", true},
		{"unknown command", []string{"bogus"}, `unknown command "bogus"`, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			run(context.Background(), tc.args, &stdout, &stderr, nil)
			assert.Equal(t, 1, strings.Count(stderr.String(), "Error: "), stderr.String())
			assert.Regexp(t, `(?m)^Error: .*`+regexp.QuoteMeta(tc.message), stderr.String())
			assert.Equal(t, tc.wantUsage, strings.Contains(stderr.String(), "Usage:"), stderr.String())
			assert.Empty(t, stdout.String(), "diagnostics never reach stdout")
		})
	}
}

// TestPersistentPreRun_OnlyOnRoot guards the dispatch marker: cobra runs only
// the nearest PersistentPreRun, so one on a subcommand would silently disable
// usage-error detection (and usage silencing) for that subtree.
func TestPersistentPreRun_OnlyOnRoot(t *testing.T) {
	t.Parallel()
	root := buildRootCmd(&runtimeOpts{}, io.Discard, io.Discard)
	require.NotNil(t, root.PersistentPreRun)
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			assert.Nil(t, sub.PersistentPreRun, "%s sets PersistentPreRun", sub.CommandPath())
			assert.Nil(t, sub.PersistentPreRunE, "%s sets PersistentPreRunE", sub.CommandPath())
			walk(sub)
		}
	}
	walk(root)
}
