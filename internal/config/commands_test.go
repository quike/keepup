package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.yaml.in/yaml/v3"
)

func TestCommandSpec_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    CommandSpec
		wantErr string
	}{
		{
			name: "argv map form",
			yaml: `{command: go, params: [build, "./..."]}`,
			want: CommandSpec{Command: "go", Params: []string{"build", "./..."}, IsShell: false},
		},
		{
			name: "argv map form without params",
			yaml: `{command: ls}`,
			want: CommandSpec{Command: "ls", IsShell: false},
		},
		{
			name: "bare string form is shell",
			yaml: `go test ./...`,
			want: CommandSpec{Command: "go test ./...", IsShell: true},
		},
		{
			name: "multiline script form is shell",
			yaml: "|\n  echo one\n  echo two\n",
			want: CommandSpec{Command: "echo one\necho two\n", IsShell: true},
		},
		{
			name:    "empty string entry rejected",
			yaml:    `""`,
			wantErr: "must not be empty",
		},
		{
			name:    "map entry with empty command rejected",
			yaml:    `{command: "", params: [x]}`,
			wantErr: `missing or empty "command"`,
		},
		{
			name:    "map entry missing command rejected",
			yaml:    `{params: [x]}`,
			wantErr: `missing or empty "command"`,
		},
		{
			name:    "unexpected key rejected",
			yaml:    `{command: go, shell: bash}`,
			wantErr: `unexpected key "shell"`,
		},
		{
			name:    "params must be a string list",
			yaml:    `{command: go, params: 5}`,
			wantErr: `"params" must be a list of strings`,
		},
		{
			name:    "sequence entry rejected",
			yaml:    `[a, b]`,
			wantErr: "must be a string or a {command, params} map",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var cs CommandSpec
			err := yaml.Unmarshal([]byte(tc.yaml), &cs)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, cs)
		})
	}
}

func TestGroup_CommandList(t *testing.T) {
	tests := []struct {
		name  string
		group Group
		want  []CommandSpec
	}{
		{
			name:  "singular exec form normalizes to one safe-exec entry",
			group: Group{Name: "g", Command: "echo", Params: []string{"hi"}},
			want:  []CommandSpec{{Command: "echo", Params: []string{"hi"}, IsShell: false}},
		},
		{
			name:  "singular shell form normalizes to one shell entry",
			group: Group{Name: "g", Command: "brew", Params: []string{"update", "-v"}, Shell: "bash"},
			want:  []CommandSpec{{Command: "brew", Params: []string{"update", "-v"}, IsShell: true}},
		},
		{
			name: "commands list is returned as-is",
			group: Group{Name: "g", Shell: "sh", Commands: []CommandSpec{
				{Command: "go", Params: []string{"build"}},
				{Command: "go test ./...", IsShell: true},
			}},
			want: []CommandSpec{
				{Command: "go", Params: []string{"build"}},
				{Command: "go test ./...", IsShell: true},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.group.CommandList())
		})
	}
}

func TestNewConfig_CommandsParsing(t *testing.T) {
	yml := `
version: 2
groups:
  - name: ci
    shell: sh
    commands:
      - { command: go, params: [build, "./..."] }
      - go test ./...
      - |
        echo one
        echo two
  - name: multi-argv
    commands:
      - { command: echo, params: [a] }
      - { command: echo, params: [b] }
  - name: single
    command: echo
    params: [hi]
flows:
  f:
    steps:
      - run: [ci, multi-argv, single]
`
	cfg, err := NewConfig([]byte(yml))
	require.NoError(t, err)

	ci := cfg.GroupByName("ci")
	require.NotNil(t, ci)
	require.Len(t, ci.CommandList(), 3)
	assert.Equal(t, CommandSpec{Command: "go", Params: []string{"build", "./..."}}, ci.CommandList()[0])
	assert.Equal(t, CommandSpec{Command: "go test ./...", IsShell: true}, ci.CommandList()[1])
	assert.True(t, ci.CommandList()[2].IsShell)
	assert.Equal(t, "echo one\necho two\n", ci.CommandList()[2].Command)

	// multiple argv entries need no shell:
	argv := cfg.GroupByName("multi-argv")
	require.NotNil(t, argv)
	require.Len(t, argv.CommandList(), 2)

	single := cfg.GroupByName("single")
	require.NotNil(t, single)
	assert.Equal(t,
		[]CommandSpec{{Command: "echo", Params: []string{"hi"}}},
		single.CommandList())
}
