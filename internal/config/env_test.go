package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeclaredEnv(t *testing.T) {
	t.Run("command beats group beats global", func(t *testing.T) {
		got := DeclaredEnv(
			map[string]string{"A": "global", "B": "global", "C": "global"},
			map[string]string{"B": "group", "C": "group"},
			map[string]string{"C": "command"},
		)
		assert.Equal(t, map[string]string{"A": "global", "B": "group", "C": "command"}, got)
	})

	t.Run("nil layers are empty", func(t *testing.T) {
		assert.Empty(t, DeclaredEnv(nil, nil, nil))
	})

	t.Run("inputs are not mutated", func(t *testing.T) {
		global := map[string]string{"A": "global"}
		DeclaredEnv(global, map[string]string{"A": "group"}, nil)
		assert.Equal(t, map[string]string{"A": "global"}, global)
	})
}
