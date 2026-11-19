package engine

import (
	"context"
	"io"
	"os"
	"os/exec"
	"time"
)

// Prober evaluates a gating predicate. A nil error means the predicate
// succeeded (exit code 0); a non-nil error means it failed. env is the declared
// (config) env; implementations layer it over the process environment.
//
// Predicates are short shell snippets (e.g. "test -f bin/app"), so they always
// run through a shell.
type Prober interface {
	Probe(ctx context.Context, script string, env map[string]string) error
}

// ShellProber runs predicates through the platform shell. Output is discarded;
// only the exit status matters. Cancellation follows the same process-group
// semantics as ShellRunner.
type ShellProber struct {
	// KillGrace is the SIGTERM-to-SIGKILL grace on cancel; zero means
	// DefaultKillGrace.
	KillGrace time.Duration
	// Groups, if set, tracks running predicates so they can be force-killed.
	Groups *ProcessGroups
}

// Probe runs script via the platform shell and returns its exit status as an
// error (nil on success).
func (p ShellProber) Probe(ctx context.Context, script string, env map[string]string) error {
	cmd := exec.CommandContext(ctx, pickShell(""), shellFlag(), script) //nolint:gosec // user-declared predicate
	cmd.Env = formatEnviron(overlay(parseEnviron(os.Environ()), env))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return runInGroup(cmd, p.KillGrace, p.Groups)
}
