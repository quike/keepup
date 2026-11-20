//go:build !windows

package engine

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
)

// A short grace keeps these real-process tests fast; the generous margin
// keeps them stable on a loaded CI runner.
const (
	testGrace  = 300 * time.Millisecond
	testMargin = 2 * time.Second
)

// shellSpec builds a shell-mode group for script. The script signals that it
// is set up by touching "$READY", whose path is returned (see cancelWhenReady).
func shellSpec(t *testing.T, script string) (g *config.Group, spec config.CommandSpec, ready string) {
	t.Helper()
	ready = filepath.Join(t.TempDir(), "ready")
	g = &config.Group{Name: "g", Command: script, Shell: defaultPosixSh, Env: map[string]string{"READY": ready}}
	return g, config.CommandSpec{Command: script, IsShell: true}, ready
}

// cancelWhenReady returns a context that is canceled once the ready file
// exists, so tests never race the shell's startup (e.g. canceling before a
// trap is installed).
func cancelWhenReady(t *testing.T, ready string) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		defer cancel()
		deadline := time.Now().Add(testMargin)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(ready); err == nil {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	return ctx
}

// len reports how many process groups are tracked.
func (p *ProcessGroups) len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.pids)
}

// requireProcessGone waits for pid to disappear; an orphan is reparented and
// reaped by init, so it may linger briefly as a zombie after SIGKILL.
func requireProcessGone(t *testing.T, pid int) {
	t.Helper()
	require.Eventually(t, func() bool {
		return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
	}, testMargin, 10*time.Millisecond, "process %d survived cancellation", pid)
}

func TestShellRunner_CancelKillsForkedGrandchildrenWithinGrace(t *testing.T) {
	t.Parallel()
	// The grandchild ignores SIGTERM and holds the stdout pipe open: without
	// process-group cancellation, Run blocked until it exited on its own.
	g, spec, ready := shellSpec(t, `trap "" TERM; sleep 30 & echo $!; touch "$READY"; wait`)
	r := &ShellRunner{Stdout: io.Discard, Stderr: io.Discard, KillGrace: testGrace}

	start := time.Now()
	rr, err := r.Run(cancelWhenReady(t, ready), g, spec, nil)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Less(t, elapsed, testGrace+testMargin)
	pid, convErr := strconv.Atoi(strings.TrimSpace(rr.Stdout))
	require.NoError(t, convErr, "grandchild pid not captured: %q", rr.Stdout)
	requireProcessGone(t, pid)
}

func TestShellRunner_CancelSendsSIGTERMSoChildCanCleanUp(t *testing.T) {
	t.Parallel()
	marker := filepath.Join(t.TempDir(), "cleaned")
	g, spec, ready := shellSpec(t, `trap 'echo cleaned > "$MARKER"; exit 0' TERM; sleep 30 & touch "$READY"; wait`)
	g.Env["MARKER"] = marker
	r := &ShellRunner{Stdout: io.Discard, Stderr: io.Discard, KillGrace: testGrace}

	_, _ = r.Run(cancelWhenReady(t, ready), g, spec, nil)

	got, err := os.ReadFile(marker)
	require.NoError(t, err, "SIGTERM trap did not run")
	assert.Equal(t, "cleaned\n", string(got))
}

func TestShellProber_CancelKillsForkedGrandchildrenWithinGrace(t *testing.T) {
	t.Parallel()
	g, _, ready := shellSpec(t, "")
	p := ShellProber{KillGrace: testGrace}

	start := time.Now()
	err := p.Probe(cancelWhenReady(t, ready), `trap "" TERM; sleep 30 & touch "$READY"; wait`, "", g.Env)

	require.Error(t, err)
	assert.Less(t, time.Since(start), testGrace+testMargin)
}

func TestProcessGroups_KillAllForceKillsRunningCommands(t *testing.T) {
	t.Parallel()
	groups := NewProcessGroups()
	g, spec, ready := shellSpec(t, `trap "" TERM; sleep 30 & touch "$READY"; wait`)
	r := &ShellRunner{Stdout: io.Discard, Stderr: io.Discard, KillGrace: testGrace, Groups: groups}

	done := make(chan error, 1)
	go func() {
		_, err := r.Run(context.Background(), g, spec, nil)
		done <- err
	}()
	require.Eventually(t, func() bool { return groups.len() == 1 }, testMargin, 5*time.Millisecond)
	require.Eventually(t, func() bool { _, err := os.Stat(ready); return err == nil }, testMargin, 5*time.Millisecond)

	groups.KillAll()

	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(testMargin):
		t.Fatal("Run did not return after KillAll")
	}
	assert.Equal(t, 0, groups.len(), "finished command must be untracked")
}

func TestProcessGroups_NilIsSafe(t *testing.T) {
	t.Parallel()
	var groups *ProcessGroups
	assert.NotPanics(t, groups.KillAll)
}
