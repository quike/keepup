// Package config defines the keepup v2 YAML schema and loads/validates it.
//
// A keepup file declares:
//   - groups: atomic, reusable command units
//   - flows:  named pipelines composed of groups, either in step mode
//     (ordered waves) or dag mode (topologically scheduled by data deps)
//
// v1 ("groups + execution" at the top level) is no longer supported.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// SchemaVersion is the only schema version this binary understands.
const SchemaVersion = 2

// Mode selects a scheduling strategy for a Flow.
type Mode string

const (
	// ModeStep is the default: explicit waves of parallel groups, with a
	// synchronization barrier between consecutive Steps.
	ModeStep Mode = "step"
	// ModeDAG schedules groups topologically from the implicit data DAG
	// formed by their {{ output.X }} references. No explicit Steps.
	ModeDAG Mode = "dag"
)

// Config is the top-level keepup configuration document.
type Config struct {
	Version  int               `yaml:"version"`
	Settings Settings          `yaml:"settings"`
	Env      map[string]string `yaml:"env,omitempty"`
	Groups   []Group           `yaml:"groups"`
	Flows    map[string]Flow   `yaml:"flows"`
	Default  string            `yaml:"default,omitempty"`
}

// Logging configures the keepup logger.
type Logging struct {
	Level  string `yaml:"level"`
	Pretty bool   `yaml:"pretty"`
}

// DefaultCacheDir is where fingerprints are stored when settings.cache-dir
// is not set.
const DefaultCacheDir = ".keepup-cache"

// CacheMethod selects how a group's input fingerprint is computed.
type CacheMethod string

const (
	// CacheHash hashes file contents — correct but reads every input.
	CacheHash CacheMethod = "hash"
	// CacheMtime uses modification time + size — fast but coarser.
	CacheMtime CacheMethod = "mtime"
)

// Settings holds global runtime settings.
type Settings struct {
	DryRun         bool    `yaml:"dry-run"`
	Logging        Logging `yaml:"logging"`
	WorkingDir     string  `yaml:"working-dir"`
	MaxConcurrency int     `yaml:"max-concurrency"`
	CacheDir       string  `yaml:"cache-dir,omitempty"`
}

// Group is an atomic, reusable command unit. Groups know nothing about flows;
// composition lives in Flow.
//
// A group runs either a single command (Command + Params) or an ordered
// Commands list; the two are mutually exclusive and both normalize to
// CommandList() so the engine has a single execution path.
//
// Shell controls how string-form commands are launched:
//   - empty: exec directly with Params as argv (safe; no shell interpretation)
//   - non-empty: pipe the command line through the named shell program (opt-in).
//
// In a Commands list, {command, params} entries are always safe argv exec and
// ignore Shell; string entries require Shell to be set.
//
// Gating (Require, SkipIf) and Cache are optional short-circuits evaluated
// before the command runs; see the engine for ordering semantics.
type Group struct {
	Name        string            `yaml:"name"`
	Command     string            `yaml:"command"`
	Params      []string          `yaml:"params,omitempty"`
	Commands    []CommandSpec     `yaml:"commands,omitempty"`
	Shell       string            `yaml:"shell,omitempty"`
	Description string            `yaml:"description,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
	Require     string            `yaml:"require,omitempty"`
	SkipIf      string            `yaml:"skip-if,omitempty"`
	Cache       *Cache            `yaml:"cache,omitempty"`
}

// Cache declares the inputs (and optional outputs) that decide whether a
// group can be skipped because nothing changed since the last run.
type Cache struct {
	Method CacheMethod `yaml:"method,omitempty"`
	Reads  []string    `yaml:"reads"`
	Writes []string    `yaml:"writes,omitempty"`
}

// UseShell reports whether the group opted into shell mode.
func (g *Group) UseShell() bool { return g.Shell != "" }

// CommandList returns the group's commands as a normalized list. When
// commands: is set it is returned as-is; otherwise the singular
// command/params pair becomes a one-element list whose shell-ness mirrors
// UseShell(). The engine, cache, and reference extraction all consume this
// accessor so singular and multi groups share a single execution path.
// The returned slice aliases the group's backing array and is intended for
// read-only use; callers must not modify it.
func (g *Group) CommandList() []CommandSpec {
	if len(g.Commands) > 0 {
		return g.Commands
	}
	return []CommandSpec{{Command: g.Command, Params: g.Params, IsShell: g.UseShell()}}
}

// Flow is a named pipeline composed of groups.
//
// Exactly one of Steps or Run must be set, matching Mode:
//   - Mode == ModeStep (default): use Steps
//   - Mode == ModeDAG:            use Run
//
// Timeout and Retries form a default control envelope applied to every group
// in the flow. In step mode a Step may override them for its own wave; in dag
// mode the flow-level values are the only knob (there are no steps).
type Flow struct {
	Description string     `yaml:"description,omitempty"`
	Mode        Mode       `yaml:"mode,omitempty"`
	Steps       []Step     `yaml:"steps,omitempty"`
	Run         []RunEntry `yaml:"run,omitempty"`
	Timeout     string     `yaml:"timeout,omitempty"`
	Retries     int        `yaml:"retries,omitempty"`
}

// Step is one execution wave inside a step-mode Flow.
//
// Timeout (a Go duration string, e.g. "30s") and Retries override the flow's
// envelope for this wave. An empty Timeout / zero Retries means "inherit".
type Step struct {
	Run     []string `yaml:"run"`
	Timeout string   `yaml:"timeout,omitempty"`
	Retries int      `yaml:"retries,omitempty"`
	// When is an optional template predicate. The step is skipped when it
	// renders to a falsey value ("", "false", "0", "no", "off"). It is
	// evaluated against the outputs of earlier steps plus the environment.
	When string `yaml:"when,omitempty"`
}

// RunEntry is one member of a dag-mode flow's run list. It is either a bare
// group-name scalar or a {group, when} mapping; both forms reference a group
// defined in top-level groups: (never an inline definition).
type RunEntry struct {
	Group string `yaml:"group"`
	// When is an optional template predicate. The group is skipped when it
	// renders falsey ("", "false", "0", "no", "off"); see the engine.
	When string `yaml:"when,omitempty"`
}

// UnmarshalYAML accepts a scalar (group name) or a mapping ({group, when}).
// Any other shape, an empty group, or an unexpected key is a load error.
func (r *RunEntry) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Value == "" {
			return fmt.Errorf("run entry: group name must not be empty")
		}
		r.Group = node.Value
		return nil
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i].Value
			valNode := node.Content[i+1]
			switch key {
			case "group":
				if valNode.Kind != yaml.ScalarNode {
					return fmt.Errorf(`run entry: "group" must be a string`)
				}
				r.Group = valNode.Value
			case "when":
				if valNode.Kind != yaml.ScalarNode {
					return fmt.Errorf(`run entry: "when" must be a string`)
				}
				r.When = valNode.Value
			default:
				return fmt.Errorf("run entry: unexpected key %q (commands are defined in groups:)", key)
			}
		}
		if r.Group == "" {
			return fmt.Errorf("run entry: missing 'group'")
		}
		return nil
	case yaml.DocumentNode, yaml.SequenceNode, yaml.AliasNode:
		return fmt.Errorf("run entry: must be a group name or a {group, when} map")
	}
	return fmt.Errorf("run entry: must be a group name or a {group, when} map")
}

// CommandSpec is one entry in a group's commands: list. The YAML shape of the
// entry selects its execution mode (same form-signals-mode rule as RunEntry):
//   - a {command, params} mapping → safe argv exec, never a shell
//   - a bare or multiline string  → a command line / script for the group's shell:
type CommandSpec struct {
	Command string   `yaml:"command" json:"command"`
	Params  []string `yaml:"params,omitempty" json:"params,omitempty"`
	// IsShell records that the entry was written in string form and therefore
	// runs through the group's shell:. Argv-form entries always exec directly
	// and ignore shell:, even when it is set.
	IsShell bool `yaml:"-" json:"shell,omitempty"`
}

// UnmarshalYAML accepts a scalar (shell command line or script) or a
// {command, params} mapping (safe argv exec). Any other shape, an empty
// string, an empty command, or an unexpected key is a load error.
func (cs *CommandSpec) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		// Deliberately rejects whitespace-only strings (stricter than RunEntry's
		// empty-only check) because a blank shell line is useless and likely a
		// YAML indentation mistake.
		if strings.TrimSpace(node.Value) == "" {
			return fmt.Errorf("commands entry: must not be empty")
		}
		cs.Command = node.Value
		cs.IsShell = true
		return nil
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i].Value
			val := node.Content[i+1]
			switch key {
			case "command":
				if val.Kind != yaml.ScalarNode {
					return fmt.Errorf(`commands entry: "command" must be a string`)
				}
				cs.Command = val.Value
			case "params":
				if err := val.Decode(&cs.Params); err != nil {
					return fmt.Errorf(`commands entry: "params" must be a list of strings: %w`, err)
				}
			default:
				return fmt.Errorf("commands entry: unexpected key %q (use a string or a {command, params} map)", key)
			}
		}
		if cs.Command == "" {
			return fmt.Errorf(`commands entry: missing or empty "command"`)
		}
		return nil
	case yaml.DocumentNode, yaml.SequenceNode, yaml.AliasNode:
		return fmt.Errorf("commands entry: must be a string or a {command, params} map")
	}
	return fmt.Errorf("commands entry: must be a string or a {command, params} map")
}

// NewConfig parses YAML bytes into a Config and validates the schema.
func NewConfig(b []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal configuration: %w", err)
	}
	if err := cfg.normalizeAndValidate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// LoadConfig reads a YAML config from disk. Supports a leading "~/" expansion.
func LoadConfig(path string) (*Config, error) {
	if path == "" {
		return nil, errors.New("config path is empty")
	}
	expanded, err := expandHome(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Clean(expanded))
	if err != nil {
		return nil, fmt.Errorf("read config file %q: %w", expanded, err)
	}
	return NewConfig(data)
}

// normalizeAndValidate enforces structural rules and runs reference checks
// for every declared flow. A single LoadConfig call surfaces every error.
func (c *Config) normalizeAndValidate() error {
	// Empty document → empty config (useful for `keepup --help` paths).
	if c.Version == 0 && len(c.Groups) == 0 && len(c.Flows) == 0 {
		return nil
	}
	if c.Version != SchemaVersion {
		return fmt.Errorf(
			"unsupported schema version %d: this binary only supports version %d",
			c.Version, SchemaVersion,
		)
	}

	groupIndex, err := c.indexGroups()
	if err != nil {
		return err
	}

	if len(c.Flows) == 0 {
		return errors.New("flows: at least one flow must be defined")
	}

	for name, flow := range c.Flows {
		if err := c.validateFlow(name, &flow, groupIndex); err != nil {
			return err
		}
	}

	if c.Default != "" {
		if _, ok := c.Flows[c.Default]; !ok {
			return fmt.Errorf("default: %q is not a declared flow", c.Default)
		}
	}
	return c.ValidateReferences()
}

func (c *Config) indexGroups() (map[string]*Group, error) {
	out := make(map[string]*Group, len(c.Groups))
	for i := range c.Groups {
		g := &c.Groups[i]
		if g.Name == "" {
			return nil, fmt.Errorf("groups[%d]: missing name", i)
		}
		if err := validateGroupCommands(i, g); err != nil {
			return nil, err
		}
		if _, dup := out[g.Name]; dup {
			return nil, fmt.Errorf("groups: duplicate name %q", g.Name)
		}
		if err := validateCache(g); err != nil {
			return nil, err
		}
		out[g.Name] = g
	}
	return out, nil
}

// validateCache normalizes and checks a group's optional cache block.
func validateCache(g *Group) error {
	if g.Cache == nil {
		return nil
	}
	if len(g.Cache.Reads) == 0 {
		return fmt.Errorf("group %q: cache.reads must list at least one path or glob", g.Name)
	}
	switch g.Cache.Method {
	case "":
		g.Cache.Method = CacheHash
	case CacheHash, CacheMtime:
		// ok
	default:
		return fmt.Errorf("group %q: unknown cache.method %q (use 'hash' or 'mtime')", g.Name, g.Cache.Method)
	}
	return nil
}

// validateGroupCommands enforces the singular-vs-list contract:
// command/params and commands: are mutually exclusive, the group must
// declare at least one command, and string-form entries (shell command
// lines / scripts) require shell: to be set. A nil Commands slice means the
// key was absent; an empty non-nil slice means an explicit `commands: []`.
func validateGroupCommands(i int, g *Group) error {
	hasSingular := g.Command != "" || len(g.Params) > 0
	switch {
	case hasSingular && g.Commands != nil:
		return fmt.Errorf("groups[%d] %q: set either 'command' or 'commands', not both", i, g.Name)
	case g.Commands == nil && g.Command == "":
		return fmt.Errorf("groups[%d] %q: missing command", i, g.Name)
	case g.Commands != nil && len(g.Commands) == 0:
		return fmt.Errorf("groups[%d] %q: 'commands' must list at least one entry", i, g.Name)
	}
	for j, cs := range g.Commands {
		if cs.IsShell && !g.UseShell() {
			return fmt.Errorf(
				"groups[%d] %q: commands[%d] is a shell command line but 'shell' is not set",
				i, g.Name, j+1,
			)
		}
	}
	return nil
}

func (c *Config) validateFlow(name string, f *Flow, groups map[string]*Group) error {
	if f.Mode == "" {
		f.Mode = ModeStep
	}
	switch f.Mode {
	case ModeStep:
		if len(f.Run) > 0 {
			return fmt.Errorf("flow %q: mode 'step' uses 'steps:', not 'run:'", name)
		}
		if len(f.Steps) == 0 {
			return fmt.Errorf("flow %q: 'steps:' is required in step mode", name)
		}
	case ModeDAG:
		if len(f.Steps) > 0 {
			return fmt.Errorf("flow %q: mode 'dag' uses 'run:', not 'steps:'", name)
		}
		if len(f.Run) == 0 {
			return fmt.Errorf("flow %q: 'run:' is required in dag mode", name)
		}
	default:
		return fmt.Errorf("flow %q: unknown mode %q (use 'step' or 'dag')", name, f.Mode)
	}
	// All referenced groups must exist.
	for _, member := range f.Members() {
		if _, ok := groups[member]; !ok {
			return fmt.Errorf("flow %q: group %q is not defined", name, member)
		}
	}
	if err := validateEnvelope(name, f); err != nil {
		return err
	}
	// Persist the normalised Mode back to the map.
	c.Flows[name] = *f
	return nil
}

// validateEnvelope checks the timeout/retries control envelope on a flow and
// its steps: timeouts must be valid Go durations and retries non-negative.
func validateEnvelope(name string, f *Flow) error {
	if err := checkTimeout(f.Timeout); err != nil {
		return fmt.Errorf("flow %q: %w", name, err)
	}
	if f.Retries < 0 {
		return fmt.Errorf("flow %q: retries must be >= 0", name)
	}
	for i := range f.Steps {
		s := &f.Steps[i]
		if err := checkTimeout(s.Timeout); err != nil {
			return fmt.Errorf("flow %q step %d: %w", name, i+1, err)
		}
		if s.Retries < 0 {
			return fmt.Errorf("flow %q step %d: retries must be >= 0", name, i+1)
		}
	}
	return nil
}

func checkTimeout(s string) error {
	if s == "" {
		return nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid timeout %q: %w", s, err)
	}
	if d < 0 {
		return fmt.Errorf("timeout %q must not be negative", s)
	}
	return nil
}

// GroupByName returns a pointer to the named Group, or nil if absent.
func (c *Config) GroupByName(name string) *Group {
	for i := range c.Groups {
		if c.Groups[i].Name == name {
			return &c.Groups[i]
		}
	}
	return nil
}

// Members returns the groups referenced by a flow, regardless of mode.
func (f *Flow) Members() []string {
	if f.Mode == ModeDAG {
		out := make([]string, len(f.Run))
		for i := range f.Run {
			out[i] = f.Run[i].Group
		}
		return out
	}
	out := make([]string, 0)
	for _, s := range f.Steps {
		out = append(out, s.Run...)
	}
	return out
}

func expandHome(path string) (string, error) {
	if !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home dir: %w", err)
	}
	return filepath.Join(home, path[2:]), nil
}
