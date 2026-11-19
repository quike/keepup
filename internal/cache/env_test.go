package cache

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
)

// envFP fingerprints one argv command under the given declared env layers.
func envFP(t *testing.T, dir string, global, group, command map[string]string) string {
	t.Helper()
	fp, err := Compute(Inputs{
		Spec:      &config.Cache{Method: config.CacheHash, Reads: []string{filepath.Join(dir, "*.go")}},
		GlobalEnv: global,
		GroupEnv:  group,
		Commands:  []config.CommandSpec{{Command: "go", Params: []string{"build"}, Env: command}},
	})
	require.NoError(t, err)
	return fp
}

func TestCompute_DeclaredEnv(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.go"), "package main\n")
	linux, darwin := map[string]string{"GOOS": "linux"}, map[string]string{"GOOS": "darwin"}

	// Every case is compared against GOOS=linux declared at the global level.
	tests := []struct {
		name                   string
		global, group, command map[string]string
		wantBust               bool
	}{
		{name: "global env change busts", global: darwin, wantBust: true},
		{name: "group env override busts", global: linux, group: darwin, wantBust: true},
		{name: "command env override busts", global: linux, command: darwin, wantBust: true},
		{name: "group shadowing global to the same effective value hits", global: darwin, group: linux},
		{name: "command shadowing group to the same effective value hits", group: darwin, command: linux},
		{name: "moving a key from global to command hits", command: linux},
	}

	baseFP := envFP(t, dir, linux, nil, nil)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := envFP(t, dir, tc.global, tc.group, tc.command)
			if tc.wantBust {
				assert.NotEqual(t, baseFP, got)
				return
			}
			assert.Equal(t, baseFP, got)
		})
	}
}

func TestCompute_DeclaredEnvOrderIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.go"), "package main\n")
	global := map[string]string{"A": "1", "B": "2", "C": "3"}
	group := map[string]string{"D": "4", "E": "5", "F": "6"}
	want := envFP(t, dir, global, group, nil)
	for range 20 {
		assert.Equal(t, want, envFP(t, dir, global, group, nil))
	}
}
