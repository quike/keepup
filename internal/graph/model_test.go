package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
)

func load(t *testing.T, body string) *config.Config {
	t.Helper()
	cfg, err := config.NewConfig([]byte(body))
	require.NoError(t, err)
	return cfg
}

const dagWhenCfg = `
version: 2
groups:
  - name: build
    command: echo
    params: [built]
  - name: test
    command: echo
    params: ['{{ output "build" }}']
  - name: deploy
    command: echo
    params: [deploying]
  - name: report
    command: echo
    params: ['{{ output "deploy" }}']
flows:
  ci:
    mode: dag
    run:
      - build
      - test
      - group: deploy
        when: '{{ eq (output "test") "pass" }}'
      - report
default: ci
`

// The scheduler orders deploy after test because its when: predicate reads
// test's output. A diagram that omits that edge contradicts what runs.
func TestBuild_WhenReferenceIsAnEdge(t *testing.T) {
	t.Parallel()
	m, err := Build(load(t, dagWhenCfg), "ci")
	require.NoError(t, err)
	assert.Contains(t, m.Edges, Edge{From: "test", To: "deploy"})
	assert.Contains(t, m.Edges, Edge{From: "build", To: "test"})
}

func TestBuild_MarksConditionalGroups(t *testing.T) {
	t.Parallel()
	m, err := Build(load(t, dagWhenCfg), "ci")
	require.NoError(t, err)

	byName := map[string]Node{}
	for _, n := range m.Nodes {
		byName[n.Name] = n
	}
	assert.True(t, byName["deploy"].Conditional)
	assert.False(t, byName["build"].Conditional)
}

const stepCfg = `
version: 2
groups:
  - name: lint
    command: echo
    params: [lint]
  - name: build
    command: echo
    params: [build]
    cache:
      reads: ["*.go"]
  - name: test
    command: echo
    params: ['{{ output "build" }}']
flows:
  ci:
    mode: step
    steps:
      - run: [lint, build]
      - run: [test]
        when: '{{ env "CI" }}'
default: ci
`

func TestBuild_StepWaves(t *testing.T) {
	t.Parallel()
	m, err := Build(load(t, stepCfg), "ci")
	require.NoError(t, err)
	require.Len(t, m.Waves, 2)
	assert.Equal(t, []string{"lint", "build"}, m.Waves[0].Groups)
	assert.Equal(t, []string{"test"}, m.Waves[1].Groups)
	assert.False(t, m.Waves[0].Conditional)
	assert.True(t, m.Waves[1].Conditional, "a step's when: gates the whole wave")
}

// Step mode gets its ordering from waves, but data edges are still worth
// drawing: they say why two groups in different waves are related.
func TestBuild_StepDataEdges(t *testing.T) {
	t.Parallel()
	m, err := Build(load(t, stepCfg), "ci")
	require.NoError(t, err)
	assert.Contains(t, m.Edges, Edge{From: "build", To: "test"})
}

func TestBuild_MarksCacheableGroups(t *testing.T) {
	t.Parallel()
	m, err := Build(load(t, stepCfg), "ci")
	require.NoError(t, err)

	byName := map[string]Node{}
	for _, n := range m.Nodes {
		byName[n.Name] = n
	}
	assert.True(t, byName["build"].Cacheable)
	assert.False(t, byName["lint"].Cacheable)
}

func TestBuild_UnknownFlow(t *testing.T) {
	t.Parallel()
	_, err := Build(load(t, stepCfg), "nope")
	require.Error(t, err)
}

// Nodes and edges must not reorder between runs, or every regenerated diagram
// shows a spurious diff.
func TestBuild_IsDeterministic(t *testing.T) {
	t.Parallel()
	cfg := load(t, dagWhenCfg)
	want, err := Build(cfg, "ci")
	require.NoError(t, err)
	for range 20 {
		got, err := Build(cfg, "ci")
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}
