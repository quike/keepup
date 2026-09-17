package graph

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func render(t *testing.T, cfgBody, flow string, f Format) string {
	t.Helper()
	m, err := Build(load(t, cfgBody), flow)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, Render(&buf, f, m))
	return buf.String()
}

func TestRender_MermaidDAG(t *testing.T) {
	t.Parallel()
	got := render(t, dagWhenCfg, "ci", FormatMermaid)

	assert.Contains(t, got, "graph TD")
	assert.Contains(t, got, "test --> deploy", "the when: dependency must be drawn")
	assert.Contains(t, got, "build --> test")
}

func TestRender_MermaidStepWavesAsSubgraphs(t *testing.T) {
	t.Parallel()
	got := render(t, stepCfg, "ci", FormatMermaid)

	assert.Contains(t, got, `subgraph wave1["step 1"]`)
	assert.Contains(t, got, `subgraph wave2["step 2 (conditional)"]`,
		"a when:-gated wave says so in its label")
	assert.Contains(t, got, "wave1 -.-> wave2", "the barrier between waves is drawn")
	assert.Equal(t, 2, strings.Count(got, "end"), "each subgraph is closed")
}

func TestRender_DotStepWavesAsClusters(t *testing.T) {
	t.Parallel()
	got := render(t, stepCfg, "ci", FormatDot)

	assert.Contains(t, got, "digraph")
	assert.Contains(t, got, "subgraph cluster_wave1")
	assert.Contains(t, got, `label="step 1"`)
}

// Unconnected clusters get laid out side by side, which reads as "these run in
// parallel" — the very thing the wave overlay exists to correct. Graphviz needs
// compound edges to order them.
func TestRender_DotOrdersWaves(t *testing.T) {
	t.Parallel()
	got := render(t, stepCfg, "ci", FormatDot)

	assert.Contains(t, got, "compound=true",
		"required for cluster-to-cluster edges")
	assert.Contains(t, got, "ltail=cluster_wave1")
	assert.Contains(t, got, "lhead=cluster_wave2")
}

func TestRender_DotDAGHasNoClusters(t *testing.T) {
	t.Parallel()
	got := render(t, dagWhenCfg, "ci", FormatDot)
	assert.NotContains(t, got, "cluster_")
	assert.Contains(t, got, `"test" -> "deploy"`)
}

// Shape carries cacheable, border style carries conditional, so a group can be
// both without the two cues colliding.
func TestRender_AnnotationsAreOrthogonal(t *testing.T) {
	t.Parallel()

	t.Run("mermaid marks a cacheable group with a cylinder", func(t *testing.T) {
		got := render(t, stepCfg, "ci", FormatMermaid)
		assert.Contains(t, got, "build[(")
		assert.Contains(t, got, "lint[", "a plain group stays a rectangle")
		assert.NotContains(t, got, "lint[(")
	})

	t.Run("mermaid dashes a conditional group", func(t *testing.T) {
		got := render(t, dagWhenCfg, "ci", FormatMermaid)
		assert.Contains(t, got, "stroke-dasharray")
		assert.Contains(t, got, "class deploy conditional")
	})

	t.Run("dot marks both", func(t *testing.T) {
		got := render(t, dagWhenCfg, "ci", FormatDot)
		assert.Contains(t, got, "style=dashed")
	})
}

func TestRender_IncludesDescriptions(t *testing.T) {
	t.Parallel()
	cfg := `
version: 2
groups:
  - name: a
    description: "does a thing"
    command: echo
    params: [a]
flows:
  f:
    steps:
      - run: [a]
default: f
`
	assert.Contains(t, render(t, cfg, "f", FormatMermaid), "does a thing")
}

// A group named "end" would otherwise emit `end["end"]` inside a subgraph that
// is itself terminated by a line reading `end`.
func TestRender_MermaidKeywordGroupName(t *testing.T) {
	t.Parallel()
	cfg := `
version: 2
groups:
  - name: end
    command: echo
    params: [done]
flows:
  f:
    steps:
      - run: [end]
default: f
`
	got := render(t, cfg, "f", FormatMermaid)
	assert.NotContains(t, got, "\n    end[", "a bare keyword id would close the subgraph")
	assert.Contains(t, got, `"end"`, "the label still shows the real name")
}

func TestIdent(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"build":       "build",
		"global-env":  "global_env",
		"build:linux": "build_linux",
		"a.b.c":       "a_b_c",
	}
	for in, want := range tests {
		assert.Equal(t, want, ident(in), "input=%q", in)
	}
}

func TestRender_UnknownFormat(t *testing.T) {
	t.Parallel()
	m, err := Build(load(t, stepCfg), "ci")
	require.NoError(t, err)
	require.Error(t, Render(&bytes.Buffer{}, Format("svg"), m))
}

// Regenerating a checked-in diagram must not produce a spurious diff.
func TestRender_IsDeterministic(t *testing.T) {
	t.Parallel()
	for _, f := range []Format{FormatMermaid, FormatDot} {
		want := render(t, dagWhenCfg, "ci", f)
		for range 10 {
			assert.Equal(t, want, render(t, dagWhenCfg, "ci", f))
		}
	}
}

// Group names are user-supplied; they must not be able to break the output
// syntax of either format.
func TestRender_SanitizesNames(t *testing.T) {
	t.Parallel()
	cfg := `
version: 2
groups:
  - name: my-group.v2
    command: echo
    params: [x]
flows:
  f:
    steps:
      - run: ["my-group.v2"]
default: f
`
	got := render(t, cfg, "f", FormatMermaid)
	assert.Contains(t, got, "my_group_v2", "identifier is sanitized")
	assert.Contains(t, got, "my-group.v2", "label keeps the real name")
}
