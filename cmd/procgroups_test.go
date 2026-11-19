package cmd

import (
	"bytes"
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/engine"
	"github.com/quike/keepup/internal/logger"
)

const sleeperCfg = `
version: 2
groups:
  - name: sleeper
    command: sleep
    params: ["30"]
default: f
flows:
  f:
    mode: step
    steps:
      - run: ["sleeper"]
`

// requireKilledViaTracker runs fn in the background and force-kills groups
// until fn returns. It only returns promptly if the commands fn starts are
// registered with groups, i.e. if the CLI wired WithProcessGroups.
func requireKilledViaTracker(t *testing.T, groups *engine.ProcessGroups, fn func() error) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("posix-only test")
	}
	done := make(chan error, 1)
	go func() { done <- fn() }()

	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			require.Error(t, err, "the killed command must fail the flow")
			return
		case <-tick.C:
			groups.KillAll()
		case <-deadline:
			t.Fatal("command was not registered with the process-group tracker")
		}
	}
}

func TestRunCmd_RegistersProcessGroups(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, sleeperCfg)
	groups := engine.NewProcessGroups()
	var out bytes.Buffer
	root := newRootCmdWithProcessGroups(&out, &out, groups)
	root.SetArgs([]string{"run", "--no-cache", "--config", cfgPath})

	requireKilledViaTracker(t, groups, root.Execute)
}

func TestWatchOnChange_RegistersProcessGroupsEveryTick(t *testing.T) {
	t.Parallel()
	cfg, err := config.LoadConfig(writeTempConfig(t, sleeperCfg))
	require.NoError(t, err)
	groups := engine.NewProcessGroups()
	opts := &runtimeOpts{cfg: cfg, log: logger.Nop(), procGroups: groups}
	onChange := buildOnChange(nil, opts, "f")

	// Two ticks: each builds a fresh engine, and each must be tracked.
	for range 2 {
		requireKilledViaTracker(t, groups, func() error {
			return onChange(context.Background(), []string{"main.go"})
		})
	}
}
