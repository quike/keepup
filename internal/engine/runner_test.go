package engine

import (
	"bytes"
	"context"
	"io"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
)

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("posix-only test")
	}
}

func TestShellRunner_DirectExecNoShell(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()

	tests := []struct {
		name    string
		params  []string
		want    string
		wantErr bool
	}{
		{
			name:   "simple echo",
			params: []string{"hello"},
			want:   "hello\n",
		},
		{
			name:   "param with spaces is one argv entry",
			params: []string{"hello world"},
			want:   "hello world\n",
		},
		{
			name:   "shell metacharacters are NOT interpreted",
			params: []string{"$(whoami)"},
			want:   "$(whoami)\n",
		},
		{
			name:   "semicolons are NOT command separators",
			params: []string{"a;rm -rf /"},
			want:   "a;rm -rf /\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			r := &ShellRunner{Stdout: &stdout, Stderr: &stderr}
			out, err := r.Run(context.Background(),
				&config.Group{Name: "g", Command: "echo"}, tc.params, nil)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, out)
		})
	}
}

func TestShellRunner_ShellModeOptIn(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	var stdout, stderr bytes.Buffer
	r := &ShellRunner{Stdout: &stdout, Stderr: &stderr}
	// Now shell substitutions DO work because the user opted in.
	out, err := r.Run(context.Background(),
		&config.Group{Name: "g", Command: "echo $((1+2))", Shell: "/bin/sh"}, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "3\n", strings.TrimLeft(out, " "))
}

func TestShellRunner_ShellModeWithParams(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	var stdout, stderr bytes.Buffer
	r := &ShellRunner{Stdout: &stdout, Stderr: &stderr}
	// With shell mode AND params, the runner joins command+params and passes
	// the result to "sh -c". This exercises the params-join branch.
	out, err := r.Run(context.Background(),
		&config.Group{Name: "g", Command: "echo", Shell: "/bin/sh"},
		[]string{"hello", "world"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "hello world\n", out)
}

func TestShellRunner_NilWritersFallbackToProcessStdio(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	// Constructing ShellRunner with zero-value writers must not panic — it
	// should silently fall back to os.Stdout/os.Stderr. We exercise the path
	// without asserting on the process stdio.
	r := &ShellRunner{} // Stdout and Stderr are both nil
	out, err := r.Run(context.Background(),
		&config.Group{Name: "g", Command: "echo"}, []string{"x"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "x\n", out)
}

func TestShellRunner_FailingCommand(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	r := &ShellRunner{Stdout: io.Discard, Stderr: io.Discard}
	_, err := r.Run(context.Background(),
		&config.Group{Name: "g", Command: "false"}, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `run "g"`)
}

func TestShellRunner_ContextCancellation(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	r := &ShellRunner{Stdout: io.Discard, Stderr: io.Discard}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := r.Run(ctx, &config.Group{Name: "g", Command: "sleep"}, []string{"5"}, nil)
	require.Error(t, err)
}

func TestShellRunner_EnvOverlayPrecedence(t *testing.T) {
	skipOnWindows(t)
	t.Setenv("X", "base")
	r := &ShellRunner{Stdout: io.Discard, Stderr: io.Discard}
	out, err := r.Run(context.Background(),
		&config.Group{
			Name:    "g",
			Command: "printenv",
			Env:     map[string]string{"X": "group"},
		},
		[]string{"X"},
		map[string]string{"X": "global"},
	)
	require.NoError(t, err)
	// group env overrides global, global overrides base
	assert.Equal(t, "group\n", out)
}

func TestMergeEnvs(t *testing.T) {
	t.Parallel()
	envs := mergeEnvs(
		[]string{"A=1", "B=2", "MALFORMED"},
		map[string]string{"B": "two", "C": "3"},
		map[string]string{"A": "one"},
	)
	got := map[string]string{}
	for _, e := range envs {
		k, v, _ := strings.Cut(e, "=")
		got[k] = v
	}
	assert.Equal(t, "one", got["A"])
	assert.Equal(t, "two", got["B"])
	assert.Equal(t, "3", got["C"])
	_, malformed := got["MALFORMED"]
	assert.False(t, malformed)
}

func TestPickShell(t *testing.T) {
	t.Run("explicit override wins", func(t *testing.T) {
		assert.Equal(t, "/usr/local/bin/zsh", pickShell("/usr/local/bin/zsh"))
	})
	t.Run("SHELL env is used when override is empty", func(t *testing.T) {
		t.Setenv("SHELL", "/opt/bin/fish")
		assert.Equal(t, "/opt/bin/fish", pickShell(""))
	})
	t.Run("falls back to /bin/sh when SHELL is empty", func(t *testing.T) {
		t.Setenv("SHELL", "")
		// On non-windows this is the fallback. On windows we'd hit COMSPEC; skip.
		skipOnWindows(t)
		assert.Equal(t, "/bin/sh", pickShell(""))
	})
}

func TestShellFlag(t *testing.T) {
	t.Parallel()
	// shellFlag returns a non-empty platform-specific switch.
	assert.NotEmpty(t, shellFlag())
}
