package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseEnviron(t *testing.T) {
	t.Parallel()
	got := parseEnviron([]string{"A=1", "B=x=y", "EMPTY=", "MALFORMED"})
	assert.Equal(t, map[string]string{"A": "1", "B": "x=y", "EMPTY": ""}, got)
}

func TestFormatEnviron(t *testing.T) {
	t.Parallel()
	got := formatEnviron(map[string]string{"A": "1", "B": "x=y"})
	assert.ElementsMatch(t, []string{"A=1", "B=x=y"}, got)
}

func TestOverlay(t *testing.T) {
	t.Parallel()
	t.Run("declared beats process, other process keys survive", func(t *testing.T) {
		t.Parallel()
		got := overlay(
			map[string]string{"K": "process", "P": "p"},
			map[string]string{"K": "declared", "D": "d"},
		)
		assert.Equal(t, map[string]string{"K": "declared", "P": "p", "D": "d"}, got)
	})
	t.Run("nil inputs are empty", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, overlay(nil, nil))
	})
	t.Run("inputs are not mutated", func(t *testing.T) {
		t.Parallel()
		process := map[string]string{"K": "process"}
		_ = overlay(process, map[string]string{"K": "declared"})
		assert.Equal(t, map[string]string{"K": "process"}, process)
	})
}
