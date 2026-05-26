package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const flowsForGraph = `
version: 2
groups:
  - name: build
    description: "compile"
    command: echo
  - name: test
    command: echo
    params: ["{{ output.build }}"]
  - name: lint
    command: echo
default: dev
flows:
  dev:
    mode: dag
    run: [build, test, lint]
  ci:
    mode: step
    steps:
      - run: [build]
      - run: [test, lint]
`

func TestGraphCmd_DAGFlow(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, flowsForGraph)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"graph", "dev", "--config", cfgPath})
	require.NoError(t, cmd.Execute())

	got := out.String()
	assert.Contains(t, got, "graph TD")
	assert.Contains(t, got, "build -->") // edge build → test
	assert.Contains(t, got, "test")
	assert.Contains(t, got, "lint")
	// build's description must be rendered inside the node label.
	assert.Contains(t, got, "compile")
}

func TestGraphCmd_UsesDefaultFlow(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, flowsForGraph)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"graph", "--config", cfgPath})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "flow: dev")
}

func TestGraphCmd_UnknownFlow(t *testing.T) {
	t.Parallel()
	cfgPath := writeTempConfig(t, flowsForGraph)
	var out bytes.Buffer
	cmd := newRootCmd(&out, &out)
	cmd.SetArgs([]string{"graph", "missing", "--config", cfgPath})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestNodeID(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"build":       "build",
		"global-env":  "global_env",
		"build:linux": "build_linux",
		"a.b.c":       "a_b_c",
	}
	for in, want := range tests {
		assert.Equal(t, want, nodeID(in), "input=%q", in)
	}
}
