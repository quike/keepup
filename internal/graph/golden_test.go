package graph

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
)

// -update rewrites the golden files. Review the diff before committing: these
// files are the reference for what a keepup diagram looks like, not just a
// snapshot of whatever the renderer happens to emit.
var update = flag.Bool("update", false, "rewrite golden files in test-resources")

// The golden files double as worked examples: reading test-resources shows the
// exact output for a step flow and a dag flow in both formats.
func TestGolden(t *testing.T) {
	tests := []struct {
		name   string
		config string
		flow   string
		format Format
		golden string
	}{
		{"step mermaid", "config-step.yml", "ci", FormatMermaid, "step.mermaid"},
		{"step dot", "config-step.yml", "ci", FormatDot, "step.dot"},
		{"dag mermaid", "config-dag.yml", "ci", FormatMermaid, "dag.mermaid"},
		{"dag dot", "config-dag.yml", "ci", FormatDot, "dag.dot"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.LoadConfig(filepath.Join("test-resources", tc.config))
			require.NoError(t, err)
			m, err := Build(cfg, tc.flow)
			require.NoError(t, err)

			var got bytes.Buffer
			require.NoError(t, Render(&got, tc.format, m))

			path := filepath.Join("test-resources", tc.golden)
			if *update {
				require.NoError(t, os.WriteFile(path, got.Bytes(), 0o600))
				return
			}
			want, err := os.ReadFile(path)
			require.NoError(t, err, "golden file missing; regenerate with: go test ./internal/graph -update")
			assert.Equal(t, string(want), got.String())
		})
	}
}
