package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/quike/keepup/internal/config"
)

func TestWithProcessGroups_WiresDefaultRunnerAndProber(t *testing.T) {
	t.Parallel()
	groups := NewProcessGroups()
	e := New(&config.Config{}, WithProcessGroups(groups))

	runner, ok := e.runner.(*ShellRunner)
	require.True(t, ok)
	assert.Same(t, groups, runner.Groups)
	prober, ok := e.prober.(ShellProber)
	require.True(t, ok)
	assert.Same(t, groups, prober.Groups)
}

func TestWithProcessGroups_KeepsExplicitCollaborators(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{}
	p := &scriptedProber{}
	groups := NewProcessGroups()

	e := New(&config.Config{}, WithProcessGroups(groups), WithRunner(r), WithProber(p))

	assert.Same(t, r, e.runner)
	assert.Same(t, p, e.prober)
}
