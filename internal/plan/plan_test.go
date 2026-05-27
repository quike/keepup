package plan

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
)

func validCfg(t *testing.T, body string) *config.Config {
	t.Helper()
	cfg, err := config.NewConfig([]byte(body))
	require.NoError(t, err)
	return cfg
}

func TestBuild_UnknownFlow(t *testing.T) {
	cfg := validCfg(t, `
version: 2
groups:
  - { name: a, command: echo }
flows:
  f:
    steps:
      - run: [a]
`)
	_, err := Build(cfg, "missing")
	require.Error(t, err)
}

func TestBuild_StepMode(t *testing.T) {
	cfg := validCfg(t, `
version: 2
groups:
  - { name: a, command: echo }
  - { name: b, command: echo }
  - { name: c, command: echo }
flows:
  f:
    mode: step
    steps:
      - run: [a, b]
      - run: [c]
`)
	p, err := Build(cfg, "f")
	require.NoError(t, err)
	assert.Equal(t, config.ModeStep, p.Mode)
	assert.Equal(t, [][]string{{"a", "b"}, {"c"}}, p.Waves)
}

func TestBuild_DAGMode_LinearChain(t *testing.T) {
	cfg := validCfg(t, `
version: 2
groups:
  - { name: a, command: echo }
  - { name: b, command: echo, params: ["{{ output.a }}"] }
  - { name: c, command: echo, params: ["{{ output.b }}"] }
flows:
  f:
    mode: dag
    run: [a, b, c]
`)
	p, err := Build(cfg, "f")
	require.NoError(t, err)
	assert.Equal(t, config.ModeDAG, p.Mode)
	assert.Equal(t, []string{"a"}, p.Roots)
	assert.Equal(t, []string{"a"}, p.Predecessors["b"])
	assert.Equal(t, []string{"b"}, p.Predecessors["c"])
	assert.Equal(t, []string{"b"}, p.Successors["a"])
	assert.Equal(t, []string{"c"}, p.Successors["b"])
}

func TestBuild_DAGMode_DiamondShape(t *testing.T) {
	// a → b, a → c, b+c → d  (the classic diamond).
	cfg := validCfg(t, `
version: 2
groups:
  - { name: a, command: echo }
  - { name: b, command: echo, params: ["{{ output.a }}"] }
  - { name: c, command: echo, params: ["{{ output.a }}"] }
  - { name: d, command: echo, params: ["{{ output.b }}+{{ output.c }}"] }
flows:
  f:
    mode: dag
    run: [a, b, c, d]
`)
	p, err := Build(cfg, "f")
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, p.Roots)
	assert.ElementsMatch(t, []string{"b", "c"}, p.Successors["a"])
	assert.ElementsMatch(t, []string{"b", "c"}, p.Predecessors["d"])
}

func TestBuild_DAGMode_RootsWhenNoEdges(t *testing.T) {
	cfg := validCfg(t, `
version: 2
groups:
  - { name: a, command: echo }
  - { name: b, command: echo }
flows:
  f:
    mode: dag
    run: [a, b]
`)
	p, err := Build(cfg, "f")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"a", "b"}, p.Roots)
}

func TestBuildDAGWhenCreatesEdge(t *testing.T) {
	doc := `version: 2
groups:
  - {name: test, command: echo}
  - {name: deploy, command: echo}
flows:
  ci:
    mode: dag
    run:
      - test
      - group: deploy
        when: '{{ eq (output "test") "pass" }}'`
	cfg, err := config.NewConfig([]byte(doc))
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	p, err := Build(cfg, "ci")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := p.Predecessors["deploy"]; len(got) != 1 || got[0] != "test" {
		t.Fatalf("deploy predecessors = %v, want [test]", got)
	}
	if p.When["deploy"] != `{{ eq (output "test") "pass" }}` {
		t.Fatalf("Plan.When[deploy] = %q, want the predicate", p.When["deploy"])
	}
	if len(p.Roots) != 1 || p.Roots[0] != "test" {
		t.Fatalf("roots = %v, want [test]", p.Roots)
	}
}

func TestBuildDAGBareStringsUnchanged(t *testing.T) {
	// Regression guard: a no-when dag flow plans identically to legacy behavior.
	doc := `version: 2
groups:
  - {name: a, command: echo}
  - {name: b, command: 'echo {{ output "a" }}'}
  - {name: c, command: echo}
flows:
  ci:
    mode: dag
    run: [a, b, c]`
	cfg, err := config.NewConfig([]byte(doc))
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	p, err := Build(cfg, "ci")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(p.When) != 0 {
		t.Fatalf("Plan.When = %v, want empty for no-when flow", p.When)
	}
	if got := p.Predecessors["b"]; len(got) != 1 || got[0] != "a" {
		t.Fatalf("b predecessors = %v, want [a]", got)
	}
	if !reflect.DeepEqual(p.Roots, []string{"a", "c"}) {
		t.Fatalf("roots = %v, want [a c]", p.Roots)
	}
}
