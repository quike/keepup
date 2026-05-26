package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
)

func TestWatchPatterns(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Groups: []config.Group{
			{Name: "build", Command: "go", Cache: &config.Cache{Reads: []string{"**/*.go", "go.mod"}}},
			{Name: "test", Command: "go", Cache: &config.Cache{Reads: []string{"**/*.go"}}}, // dup read
			{Name: "lint", Command: "golangci-lint"},                                        // no cache
		},
	}
	flow := &config.Flow{Mode: config.ModeStep, Steps: []config.Step{{Run: []string{"build", "test", "lint"}}}}

	got := watchPatterns(cfg, flow)
	// Deduped, declaration order preserved.
	assert.Equal(t, []string{"**/*.go", "go.mod"}, got)
}

func TestWatchPatterns_NoneWhenNoCache(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Groups: []config.Group{{Name: "a", Command: "echo"}},
	}
	flow := &config.Flow{Mode: config.ModeStep, Steps: []config.Step{{Run: []string{"a"}}}}
	assert.Empty(t, watchPatterns(cfg, flow))
}

func TestWatchCmd_ErrorsWithoutCacheReads(t *testing.T) {
	t.Parallel()
	// A valid flow whose groups declare no cache.reads → watch has nothing to do.
	cfg := `
version: 2
groups:
  - name: a
    command: echo
default: f
flows:
  f:
    mode: step
    steps:
      - run: [a]
`
	cfgPath := writeTempConfig(t, cfg)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"watch", "f", "--config", cfgPath})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no watchable inputs")
}

func TestWatchCmd_ErrorsOnUnknownFlow(t *testing.T) {
	t.Parallel()
	cfg := `
version: 2
groups:
  - name: a
    command: echo
    cache:
      reads: ["*.go"]
flows:
  f:
    steps:
      - run: [a]
`
	cfgPath := writeTempConfig(t, cfg)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"watch", "ghost", "--config", cfgPath})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
