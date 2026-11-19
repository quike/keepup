package engine

import (
	"context"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/result"
)

// dirRunner records the working directory each command was launched in.
type dirRunner struct {
	mu   sync.Mutex
	dirs []string
}

func (r *dirRunner) Run(_ context.Context, _ *config.Group, spec config.CommandSpec, _ map[string]string) (result.RunResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dirs = append(r.dirs, spec.Dir)
	return result.RunResult{Status: result.StatusOK}, nil
}

// dirProber records the working directory each predicate ran in.
type dirProber struct{ dirs []string }

func (p *dirProber) Probe(_ context.Context, _, dir string, _ map[string]string) error {
	p.dirs = append(p.dirs, dir)
	return nil
}

func TestEngine_CommandsRunInConfigRelativeDirs(t *testing.T) {
	root := t.TempDir()
	cfg := stepFlowCfg(t, []config.Group{{
		Name:    "a",
		Require: "true",
		Commands: []config.CommandSpec{
			{Command: "default-dir"},
			{Command: "own-dir", Dir: "web"},
			{Command: "absolute-dir", Dir: "/srv"},
		},
	}}, [][]string{{"a"}})
	cfg.Dir = root
	cfg.Settings.WorkingDir = "app"
	r, p := &dirRunner{}, &dirProber{}

	require.NoError(t, New(cfg, WithRunner(r), WithProber(p)).RunFlow(context.Background(), "f"))

	assert.Equal(t, []string{filepath.Join(root, "app"), filepath.Join(root, "web"), "/srv"}, r.dirs)
	assert.Equal(t, []string{filepath.Join(root, "app")}, p.dirs, "predicates run where commands run by default")
}

func TestShellProber_RunsInDir(t *testing.T) {
	if runtime.GOOS == osWindows {
		t.Skip("posix predicate")
	}
	dir := t.TempDir()
	require.NoError(t, writeF(filepath.Join(dir, "marker"), ""))
	require.NoError(t, ShellProber{}.Probe(context.Background(), "test -f marker", dir, nil))
	require.Error(t, ShellProber{}.Probe(context.Background(), "test -f marker", t.TempDir(), nil))
}

func TestEngine_CacheResolvesAgainstConfigDir(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "in.txt")
	require.NoError(t, writeF(input, "v1"))
	cfg := stepFlowCfg(t, []config.Group{{
		Name:    "a",
		Command: "build",
		Cache:   &config.Cache{Method: config.CacheHash, Reads: []string{"in.txt"}, Writes: []string{"in.txt"}},
	}}, [][]string{{"a"}})
	cfg.Dir = root
	run := func() int {
		r := &fakeRunner{}
		require.NoError(t, New(cfg, WithRunner(r)).RunFlow(context.Background(), "f"))
		return len(r.calls)
	}

	assert.Equal(t, 1, run(), "first run executes")
	assert.FileExists(t, filepath.Join(root, config.DefaultCacheDir, "a.json"), "default cache dir lives beside the config")
	assert.Equal(t, 0, run(), "unchanged input is a cache hit")

	require.NoError(t, writeF(input, "v2"))
	assert.Equal(t, 1, run(), "a changed input under the config dir busts the cache")
}

func TestEngine_BroadReadsSkipConfigRelativeCacheDirFromAnotherCwd(t *testing.T) {
	root := t.TempDir()
	t.Chdir(t.TempDir())
	require.NoError(t, writeF(filepath.Join(root, "in.txt"), "v1"))
	cfg := stepFlowCfg(t, []config.Group{{
		Name:    "a",
		Command: "build",
		Cache:   &config.Cache{Method: config.CacheHash, Reads: []string{"**/*"}},
	}}, [][]string{{"a"}})
	cfg.Dir = root
	run := func() int {
		r := &fakeRunner{}
		require.NoError(t, New(cfg, WithRunner(r)).RunFlow(context.Background(), "f"))
		return len(r.calls)
	}

	assert.Equal(t, 1, run(), "first run executes")
	assert.Equal(t, 0, run(), "the cache's own entries must not bust the fingerprint")
}
