package template

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"none", "plain text", nil},
		{"legacy single", "{{ output.a }}", []string{"a"}},
		{"legacy hyphenated", "{{ output.global-env }}", []string{"global-env"}},
		{"legacy multiple", "{{ output.a }}+{{ output.b }}", []string{"a", "b"}},
		{"func form", `{{ output "a" }}`, []string{"a"}},
		{"func with pipe", `{{ output "a" | trim }}`, []string{"a"}},
		{"inside if", `{{ if output "a" }}x{{ end }}`, []string{"a"}},
		{"parenthesized in if", `{{ if (output "a") }}x{{ end }}`, []string{"a"}},
		{"inside range", `{{ range output "a" }}{{ . }}{{ end }}`, []string{"a"}},
		{"inside with", `{{ with output "a" }}{{ . }}{{ end }}`, []string{"a"}},
		{"with refs both", `{{ with output "a" }}{{ output "b" }}{{ end }}`, []string{"a", "b"}},
		{"if/else both", `{{ if output "a" }}{{ output "b" }}{{ else }}{{ output "c" }}{{ end }}`, []string{"a", "b", "c"}},
		{"env not counted as ref", `{{ env "HOME" }}`, nil},
		{"mixed legacy and func", `{{ output.a }} {{ output "b" }}`, []string{"a", "b"}},
		{"dynamic name not extractable", `{{ output (printf "g%d" 1) }}`, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Refs(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestRefs_BadTemplateErrors(t *testing.T) {
	t.Parallel()
	_, err := Refs(`{{ output "x" `)
	require.Error(t, err)
}
