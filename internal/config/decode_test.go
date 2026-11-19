package config

import (
	"errors"
	"reflect"
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

// TestSchemaLocations_CoversEveryStrictStruct enforces the rule documented on
// schemaLocations: every struct the strict decoder reaches from Config must
// have a user-facing place name, or its unknown-key errors would fall back to
// Go type names. Types with their own UnmarshalYAML report keys themselves.
func TestSchemaLocations_CoversEveryStrictStruct(t *testing.T) {
	t.Parallel()
	for _, typ := range strictStructTypes(reflect.TypeFor[Config]()) {
		_, ok := schemaLocations[typ]
		assert.True(t, ok, "schemaLocations has no entry for %s", typ)
	}
}

// strictStructTypes returns every struct type reachable from root through
// fields, slices, arrays, maps and pointers whose decoding is left to the
// reflective (KnownFields) decoder.
func strictStructTypes(root reflect.Type) []reflect.Type {
	unmarshaler := reflect.TypeFor[yaml.Unmarshaler]()
	seen := map[reflect.Type]bool{}
	var out []reflect.Type
	var walk func(reflect.Type)
	walk = func(t reflect.Type) {
		k := t.Kind()
		if k == reflect.Pointer || k == reflect.Slice || k == reflect.Array {
			walk(t.Elem())
			return
		}
		if k == reflect.Map {
			walk(t.Key())
			walk(t.Elem())
			return
		}
		if k != reflect.Struct || seen[t] || reflect.PointerTo(t).Implements(unmarshaler) {
			return
		}
		seen[t] = true
		out = append(out, t)
		for f := range t.Fields() {
			if f.IsExported() && f.Tag.Get("yaml") != "-" {
				walk(f.Type)
			}
		}
	}
	walk(root)
	return out
}

func TestHumanizeDecodeError_PassesOtherErrorsThrough(t *testing.T) {
	t.Parallel()
	in := errors.New("yaml: line 1: did not find expected ',' or ']'")
	assert.Equal(t, in, humanizeDecodeError(in))
	assert.NoError(t, humanizeDecodeError(nil))
}
