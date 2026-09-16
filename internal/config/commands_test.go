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
	runCommandSpecCases(t, tests)
}

func TestCommandSpec_UnmarshalYAML_PerCommandKnobs(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    CommandSpec
		wantErr string
	}{
		{
			name: "per-command env",
			yaml: `{command: go, params: [build], env: {CGO_ENABLED: "0"}}`,
			want: CommandSpec{
				Command: "go",
				Params:  []string{"build"},
				Env:     map[string]string{"CGO_ENABLED": "0"},
			},
		},
		{
			name: "per-command dir",
			yaml: `{command: ./package.sh, dir: dist}`,
			want: CommandSpec{Command: "./package.sh", Dir: "dist"},
		},
		{
			name: "per-command silent",
			yaml: `{command: ./notify.sh, silent: true}`,
			want: CommandSpec{Command: "./notify.sh", Silent: true},
		},
		{
			name: "all three knobs together",
			yaml: `{command: ./x.sh, dir: build, silent: true, env: {A: "1", B: "2"}}`,
			want: CommandSpec{
				Command: "./x.sh",
				Dir:     "build",
				Silent:  true,
				Env:     map[string]string{"A": "1", "B": "2"},
			},
		},
		{
			name:    "empty dir rejected",
			yaml:    `{command: go, dir: ""}`,
			wantErr: `"dir" must not be empty`,
		},
		{
			name:    "empty env key rejected",
			yaml:    `{command: go, env: {"": "1"}}`,
			wantErr: `"env" keys must not be empty`,
		},
		{
			name:    "dir must be a string",
			yaml:    `{command: go, dir: [a]}`,
			wantErr: `"dir" must be a string`,
		},
		{
			name:    "silent must be a boolean",
			yaml:    `{command: go, silent: maybe}`,
			wantErr: `"silent" must be a boolean`,
		},
		{
			name:    "env must be a string map",
			yaml:    `{command: go, env: [a]}`,
			wantErr: `"env" must be a map of strings`,
		},
		{
			name: "string form stays knob-free",
			yaml: `go test ./...`,
			want: CommandSpec{Command: "go test ./...", IsShell: true},
		},
	}
	runCommandSpecCases(t, tests)
}

func runCommandSpecCases(t *testing.T, tests []struct {
	name    string
	yaml    string
	want    CommandSpec
	wantErr string
},
) {
	t.Helper()
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

func TestNewConfig_CommandsValidation(t *testing.T) {
	// wrap builds a minimal valid config around one group body.
	wrap := func(groupYAML string) string {
		return "version: 2\ngroups:\n" + groupYAML + `
flows:
  f:
    steps:
      - run: [g]
`
	}
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "both command and commands rejected",
			yaml: wrap(`  - name: g
    command: echo
    commands:
      - { command: echo, params: [x] }
`),
			wantErr: "set either 'command' or 'commands', not both",
		},
		{
			name: "params alongside commands rejected",
			yaml: wrap(`  - name: g
    params: [x]
    commands:
      - { command: echo }
`),
			wantErr: "set either 'command' or 'commands', not both",
		},
		{
			name: "explicitly empty commands list rejected",
			yaml: wrap(`  - name: g
    commands: []
`),
			wantErr: "'commands' must list at least one entry",
		},
		{
			name:    "neither command nor commands rejected",
			yaml:    wrap("  - name: g\n"),
			wantErr: "missing command",
		},
		{
			name: "string entry without shell rejected",
			yaml: wrap(`  - name: g
    commands:
      - { command: echo, params: [x] }
      - echo hi
`),
			wantErr: "commands[2] is a shell command line but 'shell' is not set",
		},
		{
			name: "script entry without shell rejected",
			yaml: wrap(`  - name: g
    commands:
      - |
        echo one
        echo two
`),
			wantErr: "commands[1] is a shell command line but 'shell' is not set",
		},
		{
			name: "argv-only list without shell is fine",
			yaml: wrap(`  - name: g
    commands:
      - { command: echo, params: [a] }
      - { command: echo, params: [b] }
`),
		},
		{
			name: "string entries with shell are fine",
			yaml: wrap(`  - name: g
    shell: sh
    commands:
      - echo hi
`),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewConfig([]byte(tc.yaml))
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestExtractRefs_CommandsEntries(t *testing.T) {
	g := &Group{Name: "g", Shell: "sh", Commands: []CommandSpec{
		{Command: "echo", Params: []string{`{{ output "a" }}`}},
		{Command: `echo {{ output "b" }}`, IsShell: true},
	}}
	refs, err := ExtractRefs(g)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, refs)
}

// ExtractRefs drives the dag scheduler's edges, load-time reference validation,
// and `keepup graph`, so dir/env templates must register there too.
func TestExtractRefs_DirAndEnv(t *testing.T) {
	g := &Group{Name: "g", Commands: []CommandSpec{{
		Command: "./package.sh",
		Dir:     `dist/{{ output "target" }}`,
		Env:     map[string]string{"SHA": `{{ output "build" }}`},
	}}}
	refs, err := ExtractRefs(g)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"target", "build"}, refs)
}

// Env keys are unordered; unstable extraction would shuffle the dag edges.
func TestExtractRefs_EnvOrderIsStable(t *testing.T) {
	g := &Group{Name: "g", Commands: []CommandSpec{{
		Command: "x",
		Env: map[string]string{
			"A": `{{ output "one" }}`, "B": `{{ output "two" }}`,
			"C": `{{ output "three" }}`, "D": `{{ output "four" }}`,
		},
	}}}
	want, err := ExtractRefs(g)
	require.NoError(t, err)
	for range 20 {
		got, err := ExtractRefs(g)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}

func TestExtractRefs_MalformedDirTemplate(t *testing.T) {
	g := &Group{Name: "g", Commands: []CommandSpec{{Command: "x", Dir: `{{ output "a" `}}}
	_, err := ExtractRefs(g)
	require.Error(t, err)
}

func TestValidateReferences_CommandsEntryForwardRef(t *testing.T) {
	yml := `
version: 2
groups:
  - name: first
    shell: sh
    commands:
      - echo {{ output "second" }}
  - name: second
    command: echo
flows:
  f:
    steps:
      - run: [first]
      - run: [second]
`
	_, err := NewConfig([]byte(yml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not produced by an earlier step")
}

// selfRefYAML wraps a group body in a minimal step-mode config.
func selfRefYAML(groupBody string) string {
	return "version: 2\ngroups:\n" + groupBody + `
flows:
  f:
    steps:
      - run: [self]
`
}

// selfRefDAGYAML wraps a group body in a minimal dag-mode config.
func selfRefDAGYAML(groupBody string) string {
	return "version: 2\ngroups:\n" + groupBody + `
flows:
  f:
    mode: dag
    run: [self]
`
}

func TestValidateReferences_StepSelfRef_MultiGroup(t *testing.T) {
	yml := selfRefYAML(`  - name: self
    shell: sh
    commands:
      - echo hi
      - echo {{ output "self" }}
`)
	_, err := NewConfig([]byte(yml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "references its own output",
		"self-ref error must mention the key phrase")
	assert.Contains(t, err.Error(), "cannot consume each other",
		"multi-command group self-ref must include the guidance hint")
}

func TestValidateReferences_DAGSelfRef_MultiGroup(t *testing.T) {
	yml := selfRefDAGYAML(`  - name: self
    shell: sh
    commands:
      - echo hi
      - echo {{ output "self" }}
`)
	_, err := NewConfig([]byte(yml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "references its own output",
		"dag self-ref error must mention the key phrase")
	assert.Contains(t, err.Error(), "cannot consume each other",
		"multi-command group dag self-ref must include the guidance hint")
}

func TestValidateReferences_DAGSelfRef_SingularGroup(t *testing.T) {
	yml := selfRefDAGYAML(`  - name: self
    shell: sh
    command: echo {{ output "self" }}
`)
	_, err := NewConfig([]byte(yml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "references its own output",
		"singular group dag self-ref error must mention the key phrase")
	assert.NotContains(t, err.Error(), "cannot consume each other",
		"singular group must not include the multi-command hint")
}

func TestLoadConfig_CommandsFixture(t *testing.T) {
	cfg, err := LoadConfig("./test-resources/config-commands-valid.yml")
	require.NoError(t, err)

	multi := cfg.GroupByName("multi")
	require.NotNil(t, multi)
	list := multi.CommandList()
	require.Len(t, list, 3)
	assert.False(t, list[0].IsShell)
	assert.True(t, list[1].IsShell)
	assert.True(t, list[2].IsShell)

	single := cfg.GroupByName("single")
	require.NotNil(t, single)
	assert.Equal(t,
		[]CommandSpec{{Command: "echo", Params: []string{"single-step"}}},
		single.CommandList())
}

func TestLoadConfig_PerCommandKnobsFixture(t *testing.T) {
	cfg, err := LoadConfig("./test-resources/config-commands-valid.yml")
	require.NoError(t, err)

	knobs := cfg.GroupByName("knobs")
	require.NotNil(t, knobs)
	list := knobs.CommandList()
	require.Len(t, list, 4)

	assert.Equal(t, map[string]string{"LAYER": "command"}, list[0].Env)
	assert.Equal(t, "/tmp", list[1].Dir)
	assert.True(t, list[2].Silent)
	assert.Equal(t, `/tmp/{{ env "HOME" }}`, list[3].Dir, "templates survive load unrendered")
	assert.Equal(t, map[string]string{"SHA": `{{ output "single" }}`}, list[3].Env)

	// Entries that declare no overrides must stay zero-valued rather than
	// inheriting the group's env by accident at load time.
	assert.Nil(t, list[1].Env)
	assert.Empty(t, list[0].Dir)
	assert.False(t, list[0].Silent)

	refs, err := ExtractRefs(knobs)
	require.NoError(t, err)
	assert.Contains(t, refs, "single", "an env template must register as a dependency")
}
