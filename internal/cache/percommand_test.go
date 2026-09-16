package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
)

// fpFor computes a fingerprint for one command against a fixed input file.
func fpFor(t *testing.T, dir string, cs config.CommandSpec) string {
	t.Helper()
	spec := &config.Cache{Method: config.CacheHash, Reads: []string{filepath.Join(dir, "*.go")}}
	fp, err := Compute(spec, "", []config.CommandSpec{cs})
	require.NoError(t, err)
	return fp
}

func TestCompute_PerCommandKnobs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.go"), "package main\n")
	base := config.CommandSpec{Command: "go", Params: []string{"build"}}

	tests := []struct {
		name     string
		spec     config.CommandSpec
		wantBust bool
	}{
		{
			name:     "env value change busts",
			spec:     config.CommandSpec{Command: "go", Params: []string{"build"}, Env: map[string]string{"CGO_ENABLED": "0"}},
			wantBust: true,
		},
		{
			name:     "dir change busts",
			spec:     config.CommandSpec{Command: "go", Params: []string{"build"}, Dir: "sub"},
			wantBust: true,
		},
		{
			name:     "silent is display-only and must NOT bust",
			spec:     config.CommandSpec{Command: "go", Params: []string{"build"}, Silent: true},
			wantBust: false,
		},
		{
			name:     "empty env map is the same as no env",
			spec:     config.CommandSpec{Command: "go", Params: []string{"build"}, Env: map[string]string{}},
			wantBust: false,
		},
	}

	baseFP := fpFor(t, dir, base)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fpFor(t, dir, tc.spec)
			if tc.wantBust {
				assert.NotEqual(t, baseFP, got)
				return
			}
			assert.Equal(t, baseFP, got)
		})
	}
}

// Go map iteration order is randomized, so an unsorted encoding would make the
// fingerprint differ between runs of the same config.
func TestCompute_EnvOrderIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.go"), "package main\n")
	spec := config.CommandSpec{
		Command: "go",
		Env:     map[string]string{"A": "1", "B": "2", "C": "3", "D": "4", "E": "5"},
	}
	want := fpFor(t, dir, spec)
	for range 20 {
		assert.Equal(t, want, fpFor(t, dir, spec))
	}
}

// Two different env maps must not hash the same just because their
// concatenated bytes could line up.
func TestCompute_EnvPairsAreUnambiguous(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.go"), "package main\n")
	a := config.CommandSpec{Command: "go", Env: map[string]string{"A": "BC"}}
	b := config.CommandSpec{Command: "go", Env: map[string]string{"AB": "C"}}
	assert.NotEqual(t, fpFor(t, dir, a), fpFor(t, dir, b))
}

// Guards the upgrade path: without this, every existing cache entry invalidates.
func TestCompute_UnchangedConfigKeepsItsFingerprint(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.go"), "package main\n")
	spec := &config.Cache{Method: config.CacheHash, Reads: []string{filepath.Join(dir, "*.go")}}

	got, err := Compute(spec, "/bin/sh", []config.CommandSpec{
		{Command: "go", Params: []string{"build", "./..."}},
		{Command: "echo done", IsShell: true},
	})
	require.NoError(t, err)

	assert.Equal(t, legacyV3(t, spec, "/bin/sh", []config.CommandSpec{
		{Command: "go", Params: []string{"build", "./..."}},
		{Command: "echo done", IsShell: true},
	}), got)
}

// legacyV3 reproduces the byte stream Compute produced before env/dir existed.
// Written out independently so the back-compat assertion cannot drift along
// with the implementation it is guarding.
func legacyV3(t *testing.T, spec *config.Cache, shell string, commands []config.CommandSpec) string {
	t.Helper()
	h := sha256.New()
	fmt.Fprintf(h, "v3\x00%s\x00", spec.Method)
	for _, c := range commands {
		fmt.Fprintf(h, "%s\x00%t\x00", c.Command, c.IsShell)
		if c.IsShell {
			fmt.Fprintf(h, "%s\x00", shell)
		}
		for _, p := range c.Params {
			fmt.Fprintf(h, "%s\x01", p)
		}
		fmt.Fprintf(h, "\x02")
	}
	files, err := resolveGlobs(spec.Reads)
	require.NoError(t, err)
	for _, f := range files {
		require.NoError(t, hashFile(h, spec.Method, f))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
