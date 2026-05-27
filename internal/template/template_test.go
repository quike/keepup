package template

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpand(t *testing.T) {
	t.Parallel()
	data := Data{
		Outputs: map[string]string{
			"build":      "bin/keepup",
			"global-env": "GLOBAL",
			"sha":        "abcdef1234567890",
			"padded":     "  v\n",
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

func TestExpand_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
	}{
		{"unclosed action", "{{ output \"x\" "},
		{"unknown function", `{{ bogusfunc "x" }}`},
		{"bad syntax", `{{ if }}`},
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
