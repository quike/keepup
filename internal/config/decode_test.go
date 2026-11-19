package config

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestHumanizeDecodeError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "top level",
			in:   []string{"line 9: field defaults not found in type config.Config"},
			want: []string{`line 9: unknown key "defaults" at top level`},
		},
		{
			name: "settings",
			in:   []string{"line 7: field max_concurrency not found in type config.Settings"},
			want: []string{`line 7: unknown key "max_concurrency" in settings`},
		},
		{
			name: "logging",
			in:   []string{"line 4: field levl not found in type config.Logging"},
			want: []string{`line 4: unknown key "levl" in logging`},
		},
		{
			name: "group",
			in:   []string{"line 9: field skipif not found in type config.Group"},
			want: []string{`line 9: unknown key "skipif" in group`},
		},
		{
			name: "cache",
			in:   []string{"line 11: field write not found in type config.Cache"},
			want: []string{`line 11: unknown key "write" in cache`},
		},
		{
			name: "flow",
			in:   []string{"line 11: field retry not found in type config.Flow"},
			want: []string{`line 11: unknown key "retry" in flow`},
		},
		{
			name: "step",
			in:   []string{"line 13: field timout not found in type config.Step"},
			want: []string{`line 13: unknown key "timout" in step`},
		},
		{
			name: "multiple errors are each translated",
			in: []string{
				"line 2: field a not found in type config.Group",
				"line 5: field b not found in type config.Flow",
			},
			want: []string{
				`line 2: unknown key "a" in group`,
				`line 5: unknown key "b" in flow`,
			},
		},
		{
			name: "non-matching entries pass through unchanged",
			in: []string{
				"line 3: cannot unmarshal !!str `x` into int",
				"line 4: field z not found in type other.Thing",
			},
			want: []string{
				"line 3: cannot unmarshal !!str `x` into int",
				"line 4: field z not found in type other.Thing",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := humanizeDecodeError(&yaml.TypeError{Errors: tc.in})
			var te *yaml.TypeError
			require.True(t, errors.As(err, &te))
			assert.Equal(t, tc.want, te.Errors)
		})
	}
}

func TestHumanizeDecodeError_PassesOtherErrorsThrough(t *testing.T) {
	t.Parallel()
	in := errors.New("yaml: line 1: did not find expected ',' or ']'")
	assert.Equal(t, in, humanizeDecodeError(in))
	assert.NoError(t, humanizeDecodeError(nil))
}
