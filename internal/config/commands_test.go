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
