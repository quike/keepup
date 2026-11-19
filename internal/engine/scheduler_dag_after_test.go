package engine

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
)

func afterCfg(t *testing.T, flow string) *config.Config {
	t.Helper()
	cfg, err := config.NewConfig([]byte(`version: 2
groups:
  - {name: lint, command: echo}
  - {name: gen, command: echo}
  - {name: build, command: echo, params: ['{{ output "gen" }}']}
  - {name: test, command: echo}
flows:
  f:
    mode: dag
` + flow))
	require.NoError(t, err)
	return cfg
}

// callOrder returns the position of each group in the runner's call log.
func callOrder(r *fakeRunner) map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]int, len(r.calls))
	for i, c := range r.calls {
		name, _, _ := strings.Cut(c, ":")
		out[name] = i
	}
	return out
}

func TestDAG_After_WaitsForPredecessor(t *testing.T) {
	cfg := afterCfg(t, `    run:
      - lint
      - {group: test, after: [lint]}`)
	// lint is slow; without the ordering edge test would start first.
	r := &fakeRunner{delays: map[string]time.Duration{"lint": 30 * time.Millisecond}}
	require.NoError(t, New(cfg, WithRunner(r)).RunFlow(context.Background(), "f"))

	order := callOrder(r)
	assert.Less(t, order["lint"], order["test"], "test must start after lint finished")
}

func TestDAG_After_SkippedPredecessorDoesNotCascade(t *testing.T) {
	cfg := afterCfg(t, `    run:
      - {group: lint, when: "false"}
      - {group: test, after: [lint]}`)
	r := &fakeRunner{}
	var buf bytes.Buffer
	e := New(cfg, WithRunner(r), WithEmitter(NewJSONEmitter(&buf)))
	require.NoError(t, e.RunFlow(context.Background(), "f"))

	ran := ranNames(r)
	assert.False(t, ran["lint"], "lint is skipped by its when:")
	assert.True(t, ran["test"], "an after: edge carries no data, so a skip must not cascade")
	assert.Equal(t, StatusSkipped, statusOf(decodeEvents(t, buf.Bytes()), "lint"))
}

// A data edge still cascades; an ordering edge downstream of the cascade does
// not extend it.
func TestDAG_After_DataCascadeStopsAtOrderingEdge(t *testing.T) {
	cfg := afterCfg(t, `    run:
      - {group: gen, when: "false"}
      - build
      - {group: test, after: [build]}`)
	r := &fakeRunner{}
	require.NoError(t, New(cfg, WithRunner(r)).RunFlow(context.Background(), "f"))

	ran := ranNames(r)
	assert.False(t, ran["gen"])
	assert.False(t, ran["build"], "build consumes gen's output and must cascade-skip")
	assert.True(t, ran["test"], "test only orders after build")
}

func TestDAG_After_FailedPredecessorBlocksDependent(t *testing.T) {
	cfg := afterCfg(t, `    run:
      - lint
      - {group: test, after: [lint]}`)
	r := &fakeRunner{errs: map[string]error{"lint": errors.New("lint failed")}}
	err := New(cfg, WithRunner(r)).RunFlow(context.Background(), "f")
	require.Error(t, err)

	assert.False(t, ranNames(r)["test"], "a failed after: predecessor blocks its dependent")
}
