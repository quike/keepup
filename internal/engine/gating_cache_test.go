package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/cache"
	"github.com/quike/keepup/internal/config"
)

func writeF(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func removeF(path string) error { return os.Remove(path) }

// scriptedProber returns a scripted result per predicate string and records calls.
type scriptedProber struct {
	mu      sync.Mutex
	results map[string]error // predicate → error (nil = success)
	calls   []string
}

func (p *scriptedProber) Probe(_ context.Context, script, _ string, _ map[string]string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, script)
	return p.results[script]
}

func TestEngine_Require(t *testing.T) {
	t.Run("requirement met → group runs", func(t *testing.T) {
		cfg := stepFlowCfg(t, []config.Group{
			{Name: "a", Command: "echo", Require: "have-tool"},
		}, [][]string{{"a"}})
		r := &fakeRunner{outputs: map[string]string{"a": "ok"}}
		p := &scriptedProber{results: map[string]error{"have-tool": nil}}
		e := New(cfg, WithRunner(r), WithProber(p))

		require.NoError(t, e.RunFlow(context.Background(), "f"))
		assert.Equal(t, []string{"a:"}, r.calls)
	})

	t.Run("requirement not met → hard error, runner not called", func(t *testing.T) {
		cfg := stepFlowCfg(t, []config.Group{
			{Name: "a", Command: "echo", Require: "have-tool"},
		}, [][]string{{"a"}})
		r := &fakeRunner{}
		p := &scriptedProber{results: map[string]error{"have-tool": errors.New("missing")}}
		e := New(cfg, WithRunner(r), WithProber(p))

		err := e.RunFlow(context.Background(), "f")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requirement")
		assert.Empty(t, r.calls)
	})
}

func TestEngine_SkipIf(t *testing.T) {
	t.Run("predicate succeeds → group skipped", func(t *testing.T) {
		cfg := stepFlowCfg(t, []config.Group{
			{Name: "a", Command: "echo", SkipIf: "already-done"},
		}, [][]string{{"a"}})
		r := &fakeRunner{}
		p := &scriptedProber{results: map[string]error{"already-done": nil}}
		e := New(cfg, WithRunner(r), WithProber(p))

		require.NoError(t, e.RunFlow(context.Background(), "f"))
		assert.Empty(t, r.calls, "runner must not be called when skip-if passes")
		// Skipped group still publishes a (status=skipped) result.
		v, ok := e.Outputs().Get("a")
		assert.True(t, ok)
		assert.Equal(t, "", v.Output)
		assert.Equal(t, "skipped", v.Status)
	})

	t.Run("predicate fails → group runs", func(t *testing.T) {
		cfg := stepFlowCfg(t, []config.Group{
			{Name: "a", Command: "echo", SkipIf: "already-done"},
		}, [][]string{{"a"}})
		r := &fakeRunner{outputs: map[string]string{"a": "ran"}}
		p := &scriptedProber{results: map[string]error{"already-done": errors.New("not done")}}
		e := New(cfg, WithRunner(r), WithProber(p))

		require.NoError(t, e.RunFlow(context.Background(), "f"))
		assert.Equal(t, []string{"a:"}, r.calls)
	})
}

// cacheGroupCfg builds a one-group step flow whose group caches readPath.
func cacheGroupCfg(t *testing.T, readPath string, writes ...string) *config.Config {
	t.Helper()
	return stepFlowCfg(t, []config.Group{
		{
			Name:    "build",
			Command: "echo",
			Cache:   &config.Cache{Method: config.CacheHash, Reads: []string{readPath}, Writes: writes},
		},
	}, [][]string{{"build"}})
}

func TestEngine_Cache_MissThenHit(t *testing.T) {
	dir := t.TempDir()
	readPath := filepath.Join(dir, "main.go")
	require.NoError(t, writeF(readPath, "package main\n"))
	store := cache.NewFileStore(filepath.Join(dir, "cache"))

	r1 := &fakeRunner{outputs: map[string]string{"build": "compiled\n"}}
	require.NoError(t, New(cacheGroupCfg(t, readPath), WithRunner(r1), WithCache(store)).RunFlow(context.Background(), "f"))
	require.Equal(t, []string{"build:"}, r1.calls)

	r2 := &fakeRunner{outputs: map[string]string{"build": "SHOULD-NOT-RUN"}}
	e2 := New(cacheGroupCfg(t, readPath), WithRunner(r2), WithCache(store))
	require.NoError(t, e2.RunFlow(context.Background(), "f"))
	assert.Empty(t, r2.calls, "cache hit must skip the runner")
	v, _ := e2.Outputs().Get("build")
	assert.Equal(t, "compiled\n", v.Output, "cache hit replays the original output")
	assert.Equal(t, "cached", v.Status)
}

func TestEngine_Cache_InputChangeBusts(t *testing.T) {
	dir := t.TempDir()
	readPath := filepath.Join(dir, "main.go")
	require.NoError(t, writeF(readPath, "package main\n"))
	store := cache.NewFileStore(filepath.Join(dir, "cache"))

	r1 := &fakeRunner{outputs: map[string]string{"build": "v1\n"}}
	require.NoError(t, New(cacheGroupCfg(t, readPath), WithRunner(r1), WithCache(store)).RunFlow(context.Background(), "f"))

	require.NoError(t, writeF(readPath, "package main // changed\n"))

	r2 := &fakeRunner{outputs: map[string]string{"build": "v2\n"}}
	e2 := New(cacheGroupCfg(t, readPath), WithRunner(r2), WithCache(store))
	require.NoError(t, e2.RunFlow(context.Background(), "f"))
	assert.Equal(t, []string{"build:"}, r2.calls, "changed input must re-run")
}

func TestEngine_Cache_NoCacheBypasses(t *testing.T) {
	dir := t.TempDir()
	readPath := filepath.Join(dir, "main.go")
	require.NoError(t, writeF(readPath, "package main\n"))
	store := cache.NewFileStore(filepath.Join(dir, "cache"))

	r1 := &fakeRunner{outputs: map[string]string{"build": "x\n"}}
	require.NoError(t, New(cacheGroupCfg(t, readPath), WithRunner(r1), WithCache(store)).RunFlow(context.Background(), "f"))

	r2 := &fakeRunner{outputs: map[string]string{"build": "x\n"}}
	e2 := New(cacheGroupCfg(t, readPath), WithRunner(r2), WithCache(store), WithNoCache(true))
	require.NoError(t, e2.RunFlow(context.Background(), "f"))
	assert.Equal(t, []string{"build:"}, r2.calls, "--no-cache forces a run")
}

func TestEngine_Cache_MissingWriteInvalidates(t *testing.T) {
	dir := t.TempDir()
	readPath := filepath.Join(dir, "main.go")
	writePath := filepath.Join(dir, "bin", "app")
	require.NoError(t, writeF(readPath, "package main\n"))
	require.NoError(t, writeF(writePath, "binary"))
	store := cache.NewFileStore(filepath.Join(dir, "cache"))
	cfg := cacheGroupCfg(t, readPath, writePath)

	r1 := &fakeRunner{outputs: map[string]string{"build": "x\n"}}
	require.NoError(t, New(cfg, WithRunner(r1), WithCache(store)).RunFlow(context.Background(), "f"))

	require.NoError(t, removeF(writePath))

	r2 := &fakeRunner{outputs: map[string]string{"build": "x\n"}}
	e2 := New(cfg, WithRunner(r2), WithCache(store))
	require.NoError(t, e2.RunFlow(context.Background(), "f"))
	assert.Equal(t, []string{"build:"}, r2.calls, "missing write must invalidate the cache")
}

func TestEngine_Cache_DryRunSkipsGatingAndCache(t *testing.T) {
	dir := t.TempDir()
	readPath := filepath.Join(dir, "main.go")
	require.NoError(t, writeF(readPath, "package main\n"))
	store := cache.NewFileStore(filepath.Join(dir, "cache"))

	r := &fakeRunner{}
	p := &scriptedProber{results: map[string]error{}}
	cfg := stepFlowCfg(t, []config.Group{
		{Name: "build", Command: "echo", Require: "x", SkipIf: "y",
			Cache: &config.Cache{Reads: []string{readPath}}},
	}, [][]string{{"build"}})
	e := New(cfg, WithRunner(r), WithProber(p), WithCache(store), WithDryRun(true))
	require.NoError(t, e.RunFlow(context.Background(), "f"))
	assert.Empty(t, r.calls)
	assert.Empty(t, p.calls, "dry-run must not evaluate predicates")
}

func TestEngine_Cache_DeclaredEnvChangeBusts(t *testing.T) {
	tests := []struct {
		name    string
		withEnv func(cfg *config.Config, goos string)
	}{
		{"global env", func(cfg *config.Config, goos string) { cfg.Env = map[string]string{"GOOS": goos} }},
		{"group env", func(cfg *config.Config, goos string) { cfg.Groups[0].Env = map[string]string{"GOOS": goos} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			readPath := filepath.Join(dir, "main.go")
			require.NoError(t, writeF(readPath, "package main\n"))
			store := cache.NewFileStore(filepath.Join(dir, "cache"))

			linux := cacheGroupCfg(t, readPath)
			tt.withEnv(linux, "linux")
			r1 := &fakeRunner{outputs: map[string]string{"build": "linux\n"}}
			require.NoError(t, New(linux, WithRunner(r1), WithCache(store)).RunFlow(context.Background(), "f"))

			darwin := cacheGroupCfg(t, readPath)
			tt.withEnv(darwin, "darwin")
			r2 := &fakeRunner{outputs: map[string]string{"build": "darwin\n"}}
			require.NoError(t, New(darwin, WithRunner(r2), WithCache(store)).RunFlow(context.Background(), "f"))
			assert.Equal(t, []string{"build:"}, r2.calls, "changed %s must re-run, not replay stale output", tt.name)
		})
	}
}

// A reads glob broad enough to match the cache directory must not let the
// store's own entries feed the fingerprint, or every save busts the next run.
func TestEngine_Cache_ReadsGlobMatchingCacheDirStillHits(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, writeF("config.json", "{}"))
	cfg := cacheGroupCfg(t, "**/*.json") // default cache-dir holds <group>.json

	r1 := &fakeRunner{outputs: map[string]string{"build": "x\n"}}
	require.NoError(t, New(cfg, WithRunner(r1)).RunFlow(context.Background(), "f"))
	require.Equal(t, []string{"build:"}, r1.calls, "first run populates the cache")

	for run := 2; run <= 3; run++ {
		r := &fakeRunner{outputs: map[string]string{"build": "x\n"}}
		require.NoError(t, New(cfg, WithRunner(r)).RunFlow(context.Background(), "f"))
		assert.Empty(t, r.calls, "run %d must hit the cache", run)
	}
}
