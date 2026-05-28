package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/result"
)

const (
	osWindows      = "windows"
	defaultPosixSh = "/bin/sh"
)

// Runner executes a single group and returns its structured RunResult.
type Runner interface {
	Run(ctx context.Context, g *config.Group, params []string, globalEnv map[string]string) (result.RunResult, error)
}

// ShellRunner executes a group via os/exec, optionally through a system shell.
//
// By default (group.Shell == false) it spawns Command directly with Params as
// argv — no shell interpretation, no injection surface. When group.Shell is
// true the runner concatenates command+params into a single string and pipes
// it through the user's preferred shell; that mode is opt-in.
type ShellRunner struct {
	// Stdout and Stderr are forwarded for live output; outputs are also captured
	// into the returned RunResult. If nil, os.Stdout/os.Stderr are used.
	Stdout io.Writer
	Stderr io.Writer
}

// NewShellRunner returns a runner wired to the process stdio.
func NewShellRunner() *ShellRunner {
	return &ShellRunner{Stdout: os.Stdout, Stderr: os.Stderr}
}

// Run honors ctx for cancellation. It captures stdout, stderr, and the
// chronologically interleaved combined stream into three buffers populated on
// the returned RunResult; ExitCode and DurationMs are also filled in.
//
// The command and arguments come from user-supplied configuration; that is
// the point of this tool. gosec G204 is suppressed for the exec call.
func (r *ShellRunner) Run(ctx context.Context, g *config.Group, params []string, globalEnv map[string]string) (result.RunResult, error) {
	cmd := r.buildCmd(ctx, g, params, globalEnv)

	captureStdout := &safeBuf{}
	captureStderr := &safeBuf{}
	captureCombined := &safeBuf{}
	stdout := r.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := r.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	cmd.Stdout = io.MultiWriter(stdout, captureStdout, captureCombined)
	cmd.Stderr = io.MultiWriter(stderr, captureStderr, captureCombined)

	start := time.Now()
	runErr := cmd.Run()
	durationMs := time.Since(start).Milliseconds()

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	rr := result.RunResult{
		Stdout:     captureStdout.String(),
		Stderr:     captureStderr.String(),
		Output:     captureCombined.String(),
		ExitCode:   exitCode,
		DurationMs: durationMs,
		Status:     resultStatusOK,
	}
	if runErr != nil {
		return rr, fmt.Errorf("run %q: %w", g.Name, runErr)
	}
	return rr, nil
}

// buildCmd assembles the exec.Cmd for a group invocation, honoring shell
// opt-in and the layered environment.
func (r *ShellRunner) buildCmd(ctx context.Context, g *config.Group, params []string, globalEnv map[string]string) *exec.Cmd {
	var cmd *exec.Cmd
	if g.UseShell() {
		shell := pickShell(g.Shell)
		full := g.Command
		if len(params) > 0 {
			full = g.Command + " " + strings.Join(params, " ")
		}
		cmd = exec.CommandContext(ctx, shell, shellFlag(), full)
	} else {
		cmd = exec.CommandContext(ctx, g.Command, params...) //nolint:gosec // user-declared command
	}
	cmd.Env = mergeEnvs(os.Environ(), globalEnv, g.Env)
	return cmd
}

// safeBuf is a goroutine-safe wrapper around bytes.Buffer; os/exec writes
// stdout and stderr from independent goroutines, so the capture buffer must
// be synchronized.
type safeBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func pickShell(override string) string {
	if override != "" {
		return override
	}
	if runtime.GOOS == osWindows {
		if v := os.Getenv("COMSPEC"); v != "" {
			return v
		}
		return "cmd.exe"
	}
	if v := os.Getenv("SHELL"); v != "" {
		return v
	}
	return defaultPosixSh
}

func shellFlag() string {
	if runtime.GOOS == osWindows {
		return "/C"
	}
	return "-c"
}

// mergeEnvs converts the base process environment plus zero or more overlay
// maps into a flat KEY=VALUE slice suitable for *exec.Cmd.Env.
func mergeEnvs(base []string, overrides ...map[string]string) []string {
	merged := make(map[string]string, len(base))
	for _, raw := range base {
		k, v, ok := strings.Cut(raw, "=")
		if ok {
			merged[k] = v
		}
	}
	for _, layer := range overrides {
		maps.Copy(merged, layer)
	}
	final := make([]string, 0, len(merged))
	for k, v := range merged {
		final = append(final, fmt.Sprintf("%s=%s", k, v))
	}
	return final
}
