package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validYAML = `
version: 2

settings:
  logging:
    level: info
    pretty: true
  working-dir: /tmp
  max-concurrency: 2

env:
  KEY: "value"

groups:
  - name: group1
    description: "produces a value"
    command: echo
    params: ["hello"]
  - name: group2
    description: "consumes group1"
    command: echo
    params: ["{{ output.group1 }}"]

default: ci

flows:
  ci:
    description: "build then test"
    mode: step
    steps:
      - run: ["group1"]
      - run: ["group2"]
`

func TestNewConfig_Valid(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		assertion func(t *testing.T, cfg *Config)
	}{
		{
			name: "valid v2 with step flow",
			yaml: validYAML,
			assertion: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 2, cfg.Version)
				assert.Equal(t, "info", cfg.Settings.Logging.Level)
				assert.Equal(t, "/tmp", cfg.Settings.WorkingDir)
				assert.Equal(t, 2, cfg.Settings.MaxConcurrency)
				require.Len(t, cfg.Groups, 2)
				require.Contains(t, cfg.Flows, "ci")
				assert.Equal(t, ModeStep, cfg.Flows["ci"].Mode)
				assert.Equal(t, "ci", cfg.Default)
				assert.Equal(t, "value", cfg.Env["KEY"])
			},
		},
		{
			name: "wholly empty document is accepted",
			yaml: "",
			assertion: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 0, cfg.Version)
				assert.Nil(t, cfg.Groups)
				assert.Nil(t, cfg.Flows)
			},
		},
		{
			name: "dry-run flag honored",
			yaml: "version: 2\nsettings:\n  dry-run: true\ngroups:\n  - name: x\n    command: echo\nflows:\n  f:\n    steps:\n      - run: [x]\n",
			assertion: func(t *testing.T, cfg *Config) {
				assert.True(t, cfg.Settings.DryRun)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := NewConfig([]byte(tc.yaml))
			require.NoError(t, err)
			tc.assertion(t, cfg)
		})
	}
}

func TestNewConfig_Errors(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name:    "v1 schema rejected",
			yaml:    "version: 1\ngroups:\n  - name: x\n    command: echo\n",
			wantErr: "unsupported schema version 1",
		},
		{
			name:    "missing version with content rejected",
			yaml:    "groups:\n  - name: x\n    command: echo\n",
			wantErr: "unsupported schema version 0",
		},
		{
			name:    "invalid YAML",
			yaml:    "invalid_yaml: [",
			wantErr: "unmarshal configuration",
		},
		{
			name: "duplicate group name",
			yaml: `version: 2
groups:
  - name: x
    command: echo
  - name: x
    command: ls
flows:
  f:
    steps:
      - run: [x]
`,
			wantErr: "duplicate name",
		},
		{
			name:    "missing flows",
			yaml:    "version: 2\ngroups:\n  - name: x\n    command: echo\n",
			wantErr: "at least one flow must be defined",
		},
		{
			name:    "flow references undefined group",
			yaml:    "version: 2\ngroups:\n  - name: x\n    command: echo\nflows:\n  f:\n    steps:\n      - run: [missing]\n",
			wantErr: "is not defined",
		},
		{
			name:    "step mode rejects 'run:' top-level",
			yaml:    "version: 2\ngroups:\n  - name: x\n    command: echo\nflows:\n  f:\n    mode: step\n    run: [x]\n",
			wantErr: "mode 'step' uses 'steps:'",
		},
		{
			name:    "dag mode rejects 'steps:'",
			yaml:    "version: 2\ngroups:\n  - name: x\n    command: echo\nflows:\n  f:\n    mode: dag\n    steps:\n      - run: [x]\n",
			wantErr: "mode 'dag' uses 'run:'",
		},
		{
			name:    "default must reference an existing flow",
			yaml:    "version: 2\ngroups:\n  - name: x\n    command: echo\ndefault: ghost\nflows:\n  f:\n    steps:\n      - run: [x]\n",
			wantErr: "not a declared flow",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewConfig([]byte(tc.yaml))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestNewConfig_CacheAndGating(t *testing.T) {
	t.Run("parses cache, skip-if, require", func(t *testing.T) {
		cfg, err := NewConfig([]byte(`
version: 2
groups:
  - name: build
    command: go
    params: [build]
    require: "command -v go"
    skip-if: "test -f bin/keepup"
    cache:
      method: hash
      reads: ["**/*.go", "go.mod"]
      writes: ["bin/keepup"]
flows:
  f:
    steps:
      - run: [build]
`))
		require.NoError(t, err)
		g := cfg.GroupByName("build")
		require.NotNil(t, g)
		assert.Equal(t, "command -v go", g.Require)
		assert.Equal(t, "test -f bin/keepup", g.SkipIf)
		require.NotNil(t, g.Cache)
		assert.Equal(t, CacheHash, g.Cache.Method)
		assert.Equal(t, []string{"**/*.go", "go.mod"}, g.Cache.Reads)
		assert.Equal(t, []string{"bin/keepup"}, g.Cache.Writes)
	})

	t.Run("cache method defaults to hash", func(t *testing.T) {
		cfg, err := NewConfig([]byte(`
version: 2
groups:
  - name: build
    command: go
    cache:
      reads: ["main.go"]
flows:
  f:
    steps:
      - run: [build]
`))
		require.NoError(t, err)
		assert.Equal(t, CacheHash, cfg.GroupByName("build").Cache.Method)
	})

	t.Run("cache without reads is rejected", func(t *testing.T) {
		_, err := NewConfig([]byte(`
version: 2
groups:
  - name: build
    command: go
    cache:
      method: hash
flows:
  f:
    steps:
      - run: [build]
`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cache.reads must list at least one")
	})

	t.Run("unknown cache method is rejected", func(t *testing.T) {
		_, err := NewConfig([]byte(`
version: 2
groups:
  - name: build
    command: go
    cache:
      method: bogus
      reads: ["main.go"]
flows:
  f:
    steps:
      - run: [build]
`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown cache.method")
	})
}

func TestNewConfig_Envelope(t *testing.T) {
	t.Run("valid timeout/retries on flow and step parse", func(t *testing.T) {
		cfg, err := NewConfig([]byte(`
version: 2
groups:
  - {name: a, command: echo}
flows:
  f:
    timeout: 1m
    retries: 3
    steps:
      - run: [a]
        timeout: 30s
        retries: 1
`))
		require.NoError(t, err)
		assert.Equal(t, "1m", cfg.Flows["f"].Timeout)
		assert.Equal(t, 3, cfg.Flows["f"].Retries)
		assert.Equal(t, "30s", cfg.Flows["f"].Steps[0].Timeout)
		assert.Equal(t, 1, cfg.Flows["f"].Steps[0].Retries)
	})

	t.Run("dag flow accepts flow-level envelope", func(t *testing.T) {
		cfg, err := NewConfig([]byte(`
version: 2
groups:
  - {name: a, command: echo}
flows:
  f:
    mode: dag
    timeout: 5s
    retries: 2
    run: [a]
`))
		require.NoError(t, err)
		assert.Equal(t, "5s", cfg.Flows["f"].Timeout)
	})

	errCases := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name:    "invalid flow timeout",
			yaml:    "version: 2\ngroups:\n  - {name: a, command: echo}\nflows:\n  f:\n    timeout: nope\n    steps:\n      - run: [a]\n",
			wantErr: "invalid timeout",
		},
		{
			name:    "invalid step timeout",
			yaml:    "version: 2\ngroups:\n  - {name: a, command: echo}\nflows:\n  f:\n    steps:\n      - run: [a]\n        timeout: 10furlongs\n",
			wantErr: "invalid timeout",
		},
		{
			name:    "negative flow retries",
			yaml:    "version: 2\ngroups:\n  - {name: a, command: echo}\nflows:\n  f:\n    retries: -1\n    steps:\n      - run: [a]\n",
			wantErr: "retries must be >= 0",
		},
		{
			name:    "negative step retries",
			yaml:    "version: 2\ngroups:\n  - {name: a, command: echo}\nflows:\n  f:\n    steps:\n      - run: [a]\n        retries: -2\n",
			wantErr: "retries must be >= 0",
		},
	}
	for _, tc := range errCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewConfig([]byte(tc.yaml))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestLoadConfig_EnvelopeResources(t *testing.T) {
	t.Run("valid envelope fixture parses with all edges", func(t *testing.T) {
		cfg, err := LoadConfig("./test-resources/config-envelope.yml")
		require.NoError(t, err)

		rel := cfg.Flows["release"]
		assert.Equal(t, "1m", rel.Timeout)
		assert.Equal(t, 2, rel.Retries)
		require.Len(t, rel.Steps, 3)
		// step a: no overrides
		assert.Empty(t, rel.Steps[0].Timeout)
		assert.Equal(t, 0, rel.Steps[0].Retries)
		// step b: timeout override, retries left at 0
		assert.Equal(t, "10s", rel.Steps[1].Timeout)
		assert.Equal(t, 0, rel.Steps[1].Retries)
		// step c: both overridden
		assert.Equal(t, "5s", rel.Steps[2].Timeout)
		assert.Equal(t, 5, rel.Steps[2].Retries)

		dag := cfg.Flows["fast-dag"]
		assert.Equal(t, ModeDAG, dag.Mode)
		assert.Equal(t, "30s", dag.Timeout)
		assert.Equal(t, 1, dag.Retries)

		bare := cfg.Flows["bare"]
		assert.Empty(t, bare.Timeout)
		assert.Equal(t, 0, bare.Retries)
	})

	t.Run("bad-timeout fixture is rejected", func(t *testing.T) {
		_, err := LoadConfig("./test-resources/config-bad-timeout.yml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid timeout")
	})
}

func TestLoadConfig_TemplateResource(t *testing.T) {
	t.Parallel()
	cfg, err := LoadConfig("./test-resources/config-template.yml")
	require.NoError(t, err)
	require.Len(t, cfg.Groups, 6)

	// Every templating form must register "producer" as a reference so the
	// flow validates (producer is in an earlier step) and the DAG is correct.
	for _, name := range []string{"legacy-consumer", "func-consumer", "sprig-consumer", "cmd-consumer"} {
		refs, rerr := ExtractRefs(cfg.GroupByName(name))
		require.NoError(t, rerr)
		assert.Equal(t, []string{"producer"}, refs, "group %q", name)
	}

	// env-consumer references no group (only env), so it has no deps.
	refs, err := ExtractRefs(cfg.GroupByName("env-consumer"))
	require.NoError(t, err)
	assert.Empty(t, refs)
}

func TestLoadConfig(t *testing.T) {
	t.Run("valid file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(validYAML), 0o600))
		cfg, err := LoadConfig(path)
		require.NoError(t, err)
		assert.Equal(t, 2, cfg.Version)
	})

	t.Run("resource file with two flows", func(t *testing.T) {
		cfg, err := LoadConfig("./test-resources/config-valid.yml")
		require.NoError(t, err)
		assert.Equal(t, 2, cfg.Version)
		require.Len(t, cfg.Groups, 3)
		require.Contains(t, cfg.Flows, "pipeline")
		require.Contains(t, cfg.Flows, "pipeline-dag")
		assert.Equal(t, ModeStep, cfg.Flows["pipeline"].Mode)
		assert.Equal(t, ModeDAG, cfg.Flows["pipeline-dag"].Mode)
	})

	t.Run("watch/cache resource file parses", func(t *testing.T) {
		cfg, err := LoadConfig("./test-resources/config-watch.yml")
		require.NoError(t, err)
		assert.Equal(t, ".keepup-cache", cfg.Settings.CacheDir)
		require.Len(t, cfg.Groups, 3)

		gen := cfg.GroupByName("generate")
		require.NotNil(t, gen)
		require.NotNil(t, gen.Cache)
		assert.Equal(t, CacheHash, gen.Cache.Method)
		assert.Equal(t, []string{"proto/**/*.proto"}, gen.Cache.Reads)
		assert.Equal(t, []string{"internal/pb/**/*.go"}, gen.Cache.Writes)

		build := cfg.GroupByName("build")
		require.NotNil(t, build)
		assert.Equal(t, "command -v echo", build.Require)

		test := cfg.GroupByName("test")
		require.NotNil(t, test)
		assert.Equal(t, "false", test.SkipIf)
		assert.Equal(t, CacheMtime, test.Cache.Method)

		require.Contains(t, cfg.Flows, "dev")
		require.Contains(t, cfg.Flows, "dev-dag")
		assert.Equal(t, "dev", cfg.Default)
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := LoadConfig("nonexistent.yaml")
		require.Error(t, err)
	})

	t.Run("invalid YAML on disk", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.yaml")
		require.NoError(t, os.WriteFile(path, []byte("invalid_yaml: ["), 0o600))
		_, err := LoadConfig(path)
		require.Error(t, err)
	})

	t.Run("invalid resource file", func(t *testing.T) {
		_, err := LoadConfig("./test-resources/config-invalid.yml")
		require.Error(t, err)
	})

	t.Run("empty file is accepted", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "empty.yaml")
		require.NoError(t, os.WriteFile(path, nil, 0o600))
		cfg, err := LoadConfig(path)
		require.NoError(t, err)
		assert.Equal(t, 0, cfg.Version)
	})

	t.Run("expands ~/ to home", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		name := "keepup-test.yaml"
		require.NoError(t, os.WriteFile(filepath.Join(home, name), []byte(validYAML), 0o600))
		cfg, err := LoadConfig("~/" + name)
		require.NoError(t, err)
		assert.Equal(t, 2, cfg.Version)
	})

	t.Run("empty path is rejected", func(t *testing.T) {
		_, err := LoadConfig("")
		require.Error(t, err)
	})

	t.Run("short non-tilde path does not panic", func(t *testing.T) {
		_, err := LoadConfig("x")
		require.Error(t, err)
	})
}

func TestLoadConfig_HomeExpansionWithMissingHomeFails(t *testing.T) {
	if home := os.Getenv("HOME"); home == "" {
		t.Skip("HOME not normally set on this platform")
	}
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	_, err := LoadConfig("~/whatever.yaml")
	require.Error(t, err)
}

func TestGroup_UseShell(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		shell string
		want  bool
	}{
		{"empty shell field means direct exec", "", false},
		{"any non-empty value opts into shell mode", "/bin/sh", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := Group{Shell: tc.shell}
			assert.Equal(t, tc.want, g.UseShell())
		})
	}
}

func TestReferenceValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{
			name:    "DAG cycle is rejected",
			path:    "./test-resources/config-cycle.yml",
			wantErr: "cycle",
		},
		{
			name:    "same-step reference is rejected",
			path:    "./test-resources/config-forward-ref.yml",
			wantErr: "not produced by an earlier step",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadConfig(tc.path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestNewConfig_RejectsMalformedTemplate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "step mode malformed param template",
			yaml: "version: 2\ngroups:\n  - name: a\n    command: echo\n    params: ['{{ bogusfunc }}']\nflows:\n  f:\n    steps:\n      - run: [a]\n", //nolint:lll // inline YAML fixture
		},
		{
			name: "dag mode malformed command template",
			yaml: "version: 2\ngroups:\n  - name: a\n    command: '{{ output \"x\" '\nflows:\n  f:\n    mode: dag\n    run: [a]\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewConfig([]byte(tc.yaml))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "parse template")
		})
	}
}

func TestNewConfig_When(t *testing.T) {
	t.Parallel()
	t.Run("valid when with earlier-step ref parses", func(t *testing.T) {
		cfg, err := NewConfig([]byte(`
version: 2
groups:
  - {name: a, command: echo}
  - {name: b, command: echo}
flows:
  f:
    steps:
      - run: [a]
      - run: [b]
        when: '{{ eq (output "a") "x" }}'
`))
		require.NoError(t, err)
		assert.Equal(t, `{{ eq (output "a") "x" }}`, cfg.Flows["f"].Steps[1].When)
	})

	sameStep := `version: 2
groups:
  - {name: a, command: echo}
flows:
  f:
    steps:
      - run: [a]
        when: '{{ output "a" }}'
`
	malformed := `version: 2
groups:
  - {name: a, command: echo}
flows:
  f:
    steps:
      - run: [a]
        when: '{{ bogusfunc }}'
`
	for name, yaml := range map[string]string{"same-step ref": sameStep, "malformed": malformed} {
		t.Run(name, func(t *testing.T) {
			_, err := NewConfig([]byte(yaml))
			require.Error(t, err)
		})
	}
}

func TestExtractRefs(t *testing.T) {
	t.Parallel()
	g := &Group{
		Command: "echo",
		Params: []string{
			"{{ output.a }} and {{ output.b }}",
			"static",
			"{{output.c}}",
			"{{   output.d   }}",
		},
	}
	got, err := ExtractRefs(g)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c", "d"}, got)
}

func TestMembers(t *testing.T) {
	t.Parallel()
	t.Run("step mode flattens steps", func(t *testing.T) {
		f := Flow{Mode: ModeStep, Steps: []Step{{Run: []string{"a", "b"}}, {Run: []string{"c"}}}}
		assert.Equal(t, []string{"a", "b", "c"}, f.Members())
	})
	t.Run("dag mode returns run set", func(t *testing.T) {
		f := Flow{Mode: ModeDAG, Run: []string{"a", "b"}}
		assert.Equal(t, []string{"a", "b"}, f.Members())
	})
}

func TestGroupByName(t *testing.T) {
	t.Parallel()
	cfg := &Config{Groups: []Group{{Name: "a"}, {Name: "b"}}}
	assert.NotNil(t, cfg.GroupByName("a"))
	assert.Nil(t, cfg.GroupByName("missing"))
}
