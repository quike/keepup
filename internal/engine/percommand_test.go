package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/result"
	"github.com/quike/keepup/internal/template"
)

func TestShellRunner_EnvPrecedence(t *testing.T) {
	skipOnWindows(t)
	t.Setenv("KEEPUP_LAYER", "process")

	tests := []struct {
		name      string
		globalEnv map[string]string
		groupEnv  map[string]string
		cmdEnv    map[string]string
		want      string
	}{
		{name: "process only", want: "process"},
		{name: "global over process", globalEnv: map[string]string{"KEEPUP_LAYER": "global"}, want: "global"},
		{
			name:      "group over global",
			globalEnv: map[string]string{"KEEPUP_LAYER": "global"},
			groupEnv:  map[string]string{"KEEPUP_LAYER": "group"},
			want:      "group",
		},
		{
			name:      "command over group",
			globalEnv: map[string]string{"KEEPUP_LAYER": "global"},
			groupEnv:  map[string]string{"KEEPUP_LAYER": "group"},
			cmdEnv:    map[string]string{"KEEPUP_LAYER": "command"},
			want:      "command",
		},
		{
			name:     "command over process with no middle layers",
			cmdEnv:   map[string]string{"KEEPUP_LAYER": "command"},
			groupEnv: nil,
			want:     "command",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			r := &ShellRunner{Stdout: &stdout, Stderr: &stderr}
			g := &config.Group{Name: "g", Command: "printenv", Env: tc.groupEnv}
			spec := config.CommandSpec{Command: "printenv", Params: []string{"KEEPUP_LAYER"}, Env: tc.cmdEnv}
			out, err := r.Run(context.Background(), g, spec, tc.globalEnv)
			require.NoError(t, err)
			assert.Equal(t, tc.want, strings.TrimSpace(out.Stdout))
		})
	}
}

func TestShellRunner_CommandEnvDoesNotLeak(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	var stdout, stderr bytes.Buffer
	r := &ShellRunner{Stdout: &stdout, Stderr: &stderr}
	g := &config.Group{Name: "g", Command: "printenv"}

	scoped := config.CommandSpec{Command: "printenv", Params: []string{"SCOPED"}, Env: map[string]string{"SCOPED": "yes"}}
	out, err := r.Run(context.Background(), g, scoped, nil)
	require.NoError(t, err)
	assert.Equal(t, "yes", strings.TrimSpace(out.Stdout))

	// printenv exits non-zero when the variable is unset.
	bare := config.CommandSpec{Command: "printenv", Params: []string{"SCOPED"}}
	out, err = r.Run(context.Background(), g, bare, nil)
	require.Error(t, err)
	assert.Empty(t, strings.TrimSpace(out.Stdout))
}

func TestShellRunner_Dir(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("x"), 0o600))

	var stdout, stderr bytes.Buffer
	r := &ShellRunner{Stdout: &stdout, Stderr: &stderr}
	g := &config.Group{Name: "g", Command: "ls"}
	out, err := r.Run(context.Background(), g,
		config.CommandSpec{Command: "ls", Dir: dir}, nil)
	require.NoError(t, err)
	assert.Contains(t, out.Stdout, "marker.txt")
}

// The alternative, `cd x && y`, would force shell: on the whole group.
func TestShellRunner_DirKeepsArgvExec(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	r := &ShellRunner{Stdout: &stdout, Stderr: &stderr}
	g := &config.Group{Name: "g", Command: "echo"} // no Shell set
	out, err := r.Run(context.Background(), g,
		config.CommandSpec{Command: "echo", Params: []string{"$(whoami)"}, Dir: dir}, nil)
	require.NoError(t, err)
	assert.Equal(t, "$(whoami)\n", out.Stdout)
}

func TestShellRunner_DirMissingIsAnError(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	var stdout, stderr bytes.Buffer
	r := &ShellRunner{Stdout: &stdout, Stderr: &stderr}
	g := &config.Group{Name: "g", Command: "ls"}
	_, err := r.Run(context.Background(), g,
		config.CommandSpec{Command: "ls", Dir: "/no/such/directory/anywhere"}, nil)
	require.Error(t, err)
}

func TestShellRunner_Silent(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()

	tests := []struct {
		name         string
		silent       bool
		wantStreamed string
	}{
		{name: "streams by default", wantStreamed: "noisy\n"},
		{name: "silent suppresses the live stream", silent: true, wantStreamed: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			r := &ShellRunner{Stdout: &stdout, Stderr: &stderr}
			g := &config.Group{Name: "g", Command: "echo"}
			out, err := r.Run(context.Background(), g,
				config.CommandSpec{Command: "echo", Params: []string{"noisy"}, Silent: tc.silent}, nil)
			require.NoError(t, err)
			assert.Equal(t, tc.wantStreamed, stdout.String())
			// Capture is unaffected either way.
			assert.Equal(t, "noisy\n", out.Stdout)
			assert.Equal(t, "noisy\n", out.Output)
		})
	}
}

func TestExpandCommands_RendersDirAndEnv(t *testing.T) {
	t.Parallel()
	e := New(&config.Config{Version: 2})
	g := &config.Group{Name: "package", Commands: []config.CommandSpec{{
		Command: "./package.sh",
		Dir:     `dist/{{ env "KEEPUP_TARGET" }}`,
		Env:     map[string]string{"SHA": `{{ output "build" }}`, "PLAIN": "kept"},
		Silent:  true,
	}}}

	got, err := e.expandCommands(g, template.Data{
		Outputs: map[string]result.RunResult{"build": {Stdout: "a31550e", Output: "a31550e"}},
		Env:     map[string]string{"KEEPUP_TARGET": "linux"},
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "dist/linux", got[0].Dir)
	assert.Equal(t, map[string]string{"SHA": "a31550e", "PLAIN": "kept"}, got[0].Env)
	assert.True(t, got[0].Silent, "non-templated fields must survive expansion")
}

func TestExpandCommands_BadDirTemplateFails(t *testing.T) {
	t.Parallel()
	e := New(&config.Config{Version: 2})
	g := &config.Group{Name: "g", Commands: []config.CommandSpec{
		{Command: "ls", Dir: "{{ bogusfunc }}"},
	}}
	_, err := e.expandCommands(g, template.Data{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expand dir")
}

func TestExpandCommands_BadEnvTemplateFails(t *testing.T) {
	t.Parallel()
	e := New(&config.Config{Version: 2})
	g := &config.Group{Name: "g", Commands: []config.CommandSpec{
		{Command: "ls", Env: map[string]string{"X": "{{ bogusfunc }}"}},
	}}
	_, err := e.expandCommands(g, template.Data{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expand env")
}

func TestShellRunner_SilentSuppressesStderrToo(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	var stdout, stderr bytes.Buffer
	r := &ShellRunner{Stdout: &stdout, Stderr: &stderr}
	g := &config.Group{Name: "g", Command: "sh", Shell: "/bin/sh"}
	out, err := r.Run(context.Background(), g,
		config.CommandSpec{Command: "echo oops 1>&2", IsShell: true, Silent: true}, nil)
	require.NoError(t, err)
	assert.Empty(t, stderr.String())
	assert.Equal(t, "oops\n", out.Stderr)
}
