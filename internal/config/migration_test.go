package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigration_V1RejectedV2Accepted pins the documented compatibility story:
// the v1 schema is intentionally rejected with a clear error, and the v2
// migration of the same content validates fully. The two fixtures in
// test-resources/migration/ are kept as a side-by-side reference for users
// migrating their own files.
func TestMigration_V1RejectedV2Accepted(t *testing.T) {
	t.Parallel()

	t.Run("v1 is rejected by design", func(t *testing.T) {
		_, err := LoadConfig("./test-resources/migration/keepup-v1.yml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported schema version 1",
			"v1 must surface an explicit schema-version error so users know how to migrate")
	})

	t.Run("v2 migration of the same content validates", func(t *testing.T) {
		cfg, err := LoadConfig("./test-resources/migration/keepup-v2.yml")
		require.NoError(t, err)
		assert.Equal(t, 2, cfg.Version)

		// All original groups survive 1:1.
		wantGroups := []string{
			"brew-update", "brew-upgrade", "brew-upgrade-casks", "brew-cleanup",
			"omf-update", "omf-reload", "fisher-update",
		}
		require.Len(t, cfg.Groups, len(wantGroups))
		for _, want := range wantGroups {
			assert.NotNil(t, cfg.GroupByName(want), "group %q must exist", want)
		}

		// The v1 execution chain becomes the `update` flow with the same
		// 6-step shape (the parallel pair preserved in step 5).
		require.Contains(t, cfg.Flows, "update")
		update := cfg.Flows["update"]
		require.Len(t, update.Steps, 6)
		assert.Equal(t, []string{"omf-update", "fisher-update"}, update.Steps[4].Run,
			"step 5 must keep the original parallel pair")

		// The migration also exposes two partial flows that the v1 schema
		// could not express in a single file.
		assert.Contains(t, cfg.Flows, "brew")
		assert.Contains(t, cfg.Flows, "fish")

		// Default flow points at the full update.
		assert.Equal(t, "update", cfg.Default)
	})
}
