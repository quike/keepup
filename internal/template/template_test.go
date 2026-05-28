package template

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/result"
)

func TestExpand(t *testing.T) {
	t.Parallel()
	data := Data{
		Outputs: map[string]result.RunResult{
			"build":      {Output: "bin/keepup"},
			"global-env": {Output: "GLOBAL"},
			"sha":        {Output: "abcdef1234567890"},
			"padded":     {Output: "  v\n"},
		},
		Env: map[string]string{"HOME": "/home/quike", "LANG": "en_US"},
	}

	tests := []struct {
		name string
		in   string
		want string
	}{
		// --- backward compatibility (legacy substring form) ---
		{"legacy single", "{{ output.build }}", "bin/keepup"},
		{"legacy no spaces", "{{output.build}}", "bin/keepup"},
		{"legacy hyphenated name", "{{ output.global-env }}", "GLOBAL"},
		{"legacy embedded", "combined: {{ output.global-env }} done", "combined: GLOBAL done"},
		{"legacy two refs", "{{ output.build }}/{{ output.sha }}", "bin/keepup/abcdef1234567890"},

		// --- output trims surrounding whitespace (matches the old expander) ---
		{"trims output whitespace", "[{{ output.padded }}]", "[v]"},

		// --- new function form ---
		{"func form", `{{ output "build" }}`, "bin/keepup"},
		{"func hyphenated", `{{ output "global-env" }}`, "GLOBAL"},
		{"env func", `{{ env "HOME" }}`, "/home/quike"},

		// --- sprig functions + pipes ---
		{"sprig trunc via pipe", `{{ output "sha" | trunc 7 }}`, "abcdef1"},
		{"sprig upper", `{{ output "global-env" | lower }}`, "global"},
		{"sprig default on empty", `{{ output "missing" | default "fallback" }}`, "fallback"},

		// --- plain strings pass through ---
		{"no template", "just text", "just text"},
		{"empty", "", ""},

		// --- unknown output renders empty (validation guards real configs) ---
		{"unknown output", `[{{ output "ghost" }}]`, "[]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewExpander().Expand(tc.in, data)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestExpand_Sprig(t *testing.T) {
	t.Parallel()
	data := Data{
		Outputs: map[string]result.RunResult{
			"name":   {Output: "Keep Up"},
			"sha":    {Output: "abcdef1234567890"},
			"csv":    {Output: "a,b,c"},
			"padded": {Output: "  spaced  "},
			"empty":  {Output: ""},
			"num":    {Output: "21"},
		},
		Env: map[string]string{"HOME": "/home/q"},
	}
	tests := []struct {
		name string
		in   string
		want string
	}{
		// case transforms
		{"upper", `{{ output "name" | upper }}`, "KEEP UP"},
		{"lower", `{{ output "name" | lower }}`, "keep up"},
		{"title", `{{ output "name" | lower | title }}`, "Keep Up"},
		{"nospace", `{{ output "name" | nospace }}`, "KeepUp"},
		// trimming / replace
		{"trim", `{{ output "padded" | trim }}`, "spaced"},
		{"replace", `{{ output "name" | replace " " "-" }}`, "Keep-Up"},
		{"substr", `{{ output "sha" | substr 0 4 }}`, "abcd"},
		{"trunc", `{{ output "sha" | trunc 3 }}`, "abc"},
		{"repeat", `{{ "ab" | repeat 3 }}`, "ababab"},
		// quoting
		{"quote", `{{ output "name" | quote }}`, `"Keep Up"`},
		{"squote", `{{ output "name" | squote }}`, "'Keep Up'"},
		// predicates → text
		{"contains true", `{{ if contains "Up" (output "name") }}yes{{ else }}no{{ end }}`, "yes"},
		{"hasPrefix false", `{{ hasPrefix "X" (output "name") }}`, "false"},
		{"hasSuffix true", `{{ hasSuffix "Up" (output "name") }}`, "true"},
		// defaults / coalesce / empty
		{"default on empty output", `{{ output "empty" | default "fallback" }}`, "fallback"},
		{"default on missing output", `{{ output "ghost" | default "fb" }}`, "fb"},
		{"coalesce", `{{ coalesce (output "empty") (output "name") }}`, "Keep Up"},
		{"ternary", `{{ ternary "T" "F" (eq (output "name") "Keep Up") }}`, "T"},
		// lists
		{"splitList index", `{{ index (splitList "," (output "csv")) 1 }}`, "b"},
		{"join list", `{{ list "x" "y" "z" | join "/" }}`, "x/y/z"},
		// math (sprig works on ints; atoi from string)
		{"add", `{{ add (output "num" | atoi) 1 }}`, "22"},
		{"mul", `{{ mul (output "num" | atoi) 2 }}`, "42"},
		// encoding / hashing (deterministic)
		{"b64enc", `{{ "hi" | b64enc }}`, "aGk="},
		{"b64 round trip", `{{ "hi" | b64enc | b64dec }}`, "hi"},
		{"sha256", `{{ "" | sha256sum }}`, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		// regex
		{"regexReplaceAll", `{{ regexReplaceAll "[0-9]+" (output "sha") "#" }}`, "abcdef#"},
		// env piped through sprig
		{"env base", `{{ env "HOME" | base }}`, "q"},
		{"env missing default", `{{ env "NOPE" | default "none" }}`, "none"},
		// chained pipeline
		{"chain", `{{ output "name" | lower | replace " " "_" | printf "v-%s" }}`, "v-keep_up"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewExpander().Expand(tc.in, data)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestExpand_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
	}{
		{"unclosed action", "{{ output \"x\" "},
		{"unknown function", `{{ bogusfunc "x" }}`},
		{"bad syntax", `{{ if }}`},
		{"render-time failure (sprig fail)", `{{ fail "boom" }}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewExpander().Expand(tc.in, Data{})
			require.Error(t, err)
		})
	}
}

func TestExpand_EnvOverridesSprigEnv(t *testing.T) {
	t.Parallel()
	// keepup's env() reads the merged config env, not the OS env.
	got, err := NewExpander().Expand(`{{ env "KEEPUP_ONLY" }}`, Data{Env: map[string]string{"KEEPUP_ONLY": "yes"}})
	require.NoError(t, err)
	assert.Equal(t, "yes", got)
}

func TestNormalize(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"{{ output.x }}":          `{{ output "x" }}`,
		"{{output.x}}":            `{{ output "x" }}`,
		"{{ output.global-env }}": `{{ output "global-env" }}`,
		"{{- output.x -}}":        `{{ output "x" }}`,
		`{{ output "x" }}`:        `{{ output "x" }}`, // function form untouched
		"no template":             "no template",
	}
	for in, want := range tests {
		assert.Equal(t, want, normalize(in), "input=%q", in)
	}
}

// TestOutputBackCompat pins the contract that `output "x"` still returns
// strings.TrimSpace(Outputs[x].Output) and works in sprig pipes — every
// existing template must keep rendering identically.
func TestOutputBackCompat(t *testing.T) {
	t.Parallel()
	exp := NewExpander()
	data := Data{
		Outputs: map[string]result.RunResult{
			"x": {Output: "  hello world  \n", Stdout: "hello world\n", Status: "ok"},
		},
	}

	got, err := exp.Expand(`{{ output "x" }}`, data)
	require.NoError(t, err)
	assert.Equal(t, "hello world", got)

	// Sprig string pipe still works because output returns a string.
	got, err = exp.Expand(`{{ output "x" | upper }}`, data)
	require.NoError(t, err)
	assert.Equal(t, "HELLO WORLD", got)
}

// TestOutNewFunction asserts that `out "x"` returns the full RunResult and
// every field is reachable via dot access.
func TestOutNewFunction(t *testing.T) {
	t.Parallel()
	exp := NewExpander()
	data := Data{
		Outputs: map[string]result.RunResult{
			"test": {
				Stdout:     "pass",
				Stderr:     "warning\n",
				Output:     "passwarning\n",
				ExitCode:   0,
				DurationMs: 73,
				Status:     "ok",
			},
		},
	}
	cases := []struct {
		expr string
		want string
	}{
		{`{{ (out "test").Stdout }}`, "pass"},
		{`{{ (out "test").Stderr }}`, "warning\n"},
		{`{{ (out "test").Output }}`, "passwarning\n"},
		{`{{ (out "test").ExitCode }}`, "0"},
		{`{{ (out "test").DurationMs }}`, "73"},
		{`{{ (out "test").Status }}`, "ok"},
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			t.Parallel()
			got, err := exp.Expand(tc.expr, data)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestOutForMissingGroup confirms the zero RunResult is returned for a name
// the engine never stored — Status is the empty string in that case.
func TestOutForMissingGroup(t *testing.T) {
	t.Parallel()
	exp := NewExpander()
	data := Data{Outputs: map[string]result.RunResult{}}
	got, err := exp.Expand(`{{ (out "nope").Status }}`, data)
	require.NoError(t, err)
	assert.Equal(t, "", got, "missing group should yield zero RunResult, empty Status")
}
