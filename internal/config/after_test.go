package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunEntryAfter_Parses(t *testing.T) {
	t.Parallel()
	doc := `version: 2
groups:
  - {name: lint, command: echo}
  - {name: db, command: echo}
  - {name: test, command: echo}
flows:
  f:
    mode: dag
    run:
      - lint
      - db
      - group: test
        after: [lint, db]`
	cfg, err := NewConfig([]byte(doc))
	require.NoError(t, err)
	assert.Equal(t,
		[]RunEntry{{Group: "lint"}, {Group: "db"}, {Group: "test", After: []string{"lint", "db"}}},
		cfg.Flows["f"].Run,
	)
}

func TestRunEntryAfter_Errors(t *testing.T) {
	t.Parallel()
	const groups = `version: 2
groups:
  - {name: a, command: echo}
  - {name: b, command: echo, params: ['{{ output "a" }}']}
  - {name: c, command: echo}
  - {name: outside, command: echo}
`
	tests := []struct {
		name    string
		flows   string
		wantErr string
	}{
		{
			name: "scalar instead of list",
			flows: `flows:
  f:
    mode: dag
    run:
      - a
      - {group: c, after: a}`,
			wantErr: `run entry: "after" must be a list of group names`,
		},
		{
			name: "empty name in list",
			flows: `flows:
  f:
    mode: dag
    run:
      - a
      - {group: c, after: [""]}`,
			wantErr: `run entry: "after" entries must not be empty`,
		},
		{
			name: "predecessor not in flow",
			flows: `flows:
  f:
    mode: dag
    run:
      - a
      - {group: c, after: [outside]}`,
			wantErr: `flow "f": group "c": after references "outside", but "outside" is not part of this flow`,
		},
		{
			name: "predecessor not defined",
			flows: `flows:
  f:
    mode: dag
    run:
      - a
      - {group: c, after: [ghost]}`,
			wantErr: `flow "f": group "c": after references "ghost", but no group "ghost" is defined`,
		},
		{
			name: "self reference",
			flows: `flows:
  f:
    mode: dag
    run:
      - {group: c, after: [c]}`,
			wantErr: `flow "f": group "c" cannot list itself in after`,
		},
		{
			name: "ordering-only cycle",
			flows: `flows:
  f:
    mode: dag
    run:
      - {group: a, after: [c]}
      - {group: c, after: [a]}`,
			wantErr: "cycle detected",
		},
		{
			name: "mixed data and ordering cycle",
			flows: `flows:
  f:
    mode: dag
    run:
      - {group: a, after: [b]}
      - b`,
			wantErr: "cycle detected",
		},
		{
			name: "after in a step-mode flow",
			flows: `flows:
  f:
    run:
      - a
      - {group: c, after: [a]}`,
			wantErr: `flow "f": mode 'step' uses 'steps:', not 'run:'`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewConfig([]byte(groups + tc.flows))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}
