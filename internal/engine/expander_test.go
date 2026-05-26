package engine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTemplateExpander_Expand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		template string
		outputs  map[string]string
		want     string
	}{
		{"empty template", "", nil, ""},
		{"no placeholders", "hello", map[string]string{"x": "y"}, "hello"},
		{"single placeholder", "{{ output.x }}", map[string]string{"x": "v"}, "v"},
		{"trims surrounding whitespace in value", "{{ output.x }}", map[string]string{"x": "  v\n"}, "v"},
		{"unknown key passes through", "{{ output.unknown }}", map[string]string{"x": "v"}, "{{ output.unknown }}"},
		{"multiple placeholders", "{{ output.a }} and {{ output.b }}", map[string]string{"a": "A", "b": "B"}, "A and B"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := TemplateExpander{}.Expand(tc.template, tc.outputs)
			assert.Equal(t, tc.want, got)
		})
	}
}

// FuzzExpander_Idempotent: replacing the same outputs twice never changes the result.
func FuzzExpander_Idempotent(f *testing.F) {
	f.Add("hello {{ output.x }}", "x", "value")
	f.Add("{{ output.x }}{{ output.x }}", "x", "v")
	f.Add("", "k", "")
	f.Fuzz(func(t *testing.T, template, key, value string) {
		if strings.ContainsAny(key, " }{") {
			t.Skip()
		}
		outs := map[string]string{key: value}
		once := TemplateExpander{}.Expand(template, outs)
		twice := TemplateExpander{}.Expand(once, outs)
		// If the value itself contains a placeholder, idempotence is naturally broken;
		// limit fuzz to "stable" values for the invariant to hold.
		if !strings.Contains(value, "{{ output.") {
			assert.Equal(t, once, twice)
		}
	})
}
