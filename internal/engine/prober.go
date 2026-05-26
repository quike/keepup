package engine

import (
	"context"
	"io"
	"os"
	"os/exec"
)

// Prober evaluates a gating predicate. A nil error means the predicate
// succeeded (exit code 0); a non-nil error means it failed.
//
// Predicates are short shell snippets (e.g. "test -f bin/app"), so they always
// run through a shell.
type Prober interface {
	Probe(ctx context.Context, script string, env map[string]string) error
}

// ShellProber runs predicates through the platform shell. Output is discarded;
// only the exit status matters.
type ShellProber struct{}

// Probe runs script via the platform shell and returns its exit status as an
// error (nil on success).
func (ShellProber) Probe(ctx context.Context, script string, env map[string]string) error {
	cmd := exec.CommandContext(ctx, pickShell(""), shellFlag(), script) //nolint:gosec // user-declared predicate
	cmd.Env = mergeEnvs(os.Environ(), env)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}
