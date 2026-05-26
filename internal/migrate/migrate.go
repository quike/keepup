// Package migrate converts a legacy v1 keepup file into the v2 schema.
//
// v1 declared a single top-level `execution:` list of `group:` waves. v2
// replaces that with named `flows:`. migrate maps the single execution chain
// into one step-mode flow, leaving groups, env, and settings otherwise intact.
package migrate

import (
	"bytes"
	"fmt"

	"go.yaml.in/yaml/v3"

	"github.com/quike/keepup/internal/config"
)

// DefaultFlowName is the name given to the flow synthesized from v1 `execution`.
const DefaultFlowName = "main"

// --- v1 input schema (independent of the live config package) ---

type v1Doc struct {
	Version   int               `yaml:"version"`
	Settings  v1Settings        `yaml:"settings"`
	Env       map[string]string `yaml:"env"`
	Groups    []v1Group         `yaml:"groups"`
	Execution []v1Step          `yaml:"execution"`
}

type v1Settings struct {
	Logging        v1Logging `yaml:"logging"`
	WorkingDir     string    `yaml:"working-dir"`
	MaxConcurrency int       `yaml:"max-concurrency"`
}

type v1Logging struct {
	Level  string `yaml:"level"`
	Pretty bool   `yaml:"pretty"`
}

type v1Group struct {
	Name        string            `yaml:"name"`
	Command     string            `yaml:"command"`
	Params      []string          `yaml:"params"`
	Shell       string            `yaml:"shell"`
	Description string            `yaml:"description"`
	Env         map[string]string `yaml:"env"`
}

type v1Step struct {
	Group []string `yaml:"group"`
}

// --- v2 output schema (omitempty everywhere for clean output) ---

type v2Doc struct {
	Version  int               `yaml:"version"`
	Settings *v2Settings       `yaml:"settings,omitempty"`
	Env      map[string]string `yaml:"env,omitempty"`
	Groups   []v2Group         `yaml:"groups"`
	Default  string            `yaml:"default,omitempty"`
	Flows    map[string]v2Flow `yaml:"flows"`
}

type v2Settings struct {
	Logging        *v2Logging `yaml:"logging,omitempty"`
	WorkingDir     string     `yaml:"working-dir,omitempty"`
	MaxConcurrency int        `yaml:"max-concurrency,omitempty"`
}

type v2Logging struct {
	Level  string `yaml:"level,omitempty"`
	Pretty bool   `yaml:"pretty,omitempty"`
}

type v2Group struct {
	Name        string            `yaml:"name"`
	Command     string            `yaml:"command"`
	Params      []string          `yaml:"params,omitempty"`
	Shell       string            `yaml:"shell,omitempty"`
	Description string            `yaml:"description,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
}

type v2Flow struct {
	Description string   `yaml:"description,omitempty"`
	Mode        string   `yaml:"mode"`
	Steps       []v2Step `yaml:"steps"`
}

type v2Step struct {
	Run []string `yaml:"run"`
}

// Options configures a migration.
type Options struct {
	// FlowName is the name for the flow synthesized from `execution`.
	// Defaults to DefaultFlowName when empty.
	FlowName string
}

// Migrate parses v1 YAML bytes and returns the equivalent v2 YAML document.
// The result is validated against the live v2 parser before being returned, so
// a successful migration is guaranteed to load.
func Migrate(data []byte, opts Options) ([]byte, error) {
	v1, err := parseV1(data)
	if err != nil {
		return nil, err
	}
	flowName := opts.FlowName
	if flowName == "" {
		flowName = DefaultFlowName
	}
	doc := convert(v1, flowName)

	out, err := marshal(doc)
	if err != nil {
		return nil, err
	}
	// Guarantee the output is a valid v2 document.
	if _, err := config.NewConfig(out); err != nil {
		return nil, fmt.Errorf("migrated output failed v2 validation: %w", err)
	}
	return out, nil
}

func parseV1(data []byte) (*v1Doc, error) {
	var doc v1Doc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse v1 yaml: %w", err)
	}
	if doc.Version != 1 {
		return nil, fmt.Errorf("not a v1 document: version is %d (expected 1)", doc.Version)
	}
	if len(doc.Execution) == 0 {
		return nil, fmt.Errorf("v1 document has no 'execution' block to migrate")
	}
	return &doc, nil
}

func convert(v1 *v1Doc, flowName string) *v2Doc {
	doc := &v2Doc{
		Version: 2,
		Env:     v1.Env,
		Default: flowName,
		Flows:   map[string]v2Flow{},
	}

	if s := convertSettings(v1.Settings); s != nil {
		doc.Settings = s
	}

	doc.Groups = make([]v2Group, len(v1.Groups))
	for i, g := range v1.Groups {
		// v1Group and v2Group share an identical field layout.
		doc.Groups[i] = v2Group(g)
	}

	steps := make([]v2Step, len(v1.Execution))
	for i, s := range v1.Execution {
		steps[i] = v2Step{Run: s.Group}
	}
	doc.Flows[flowName] = v2Flow{
		Description: "Migrated from v1 execution",
		Mode:        "step",
		Steps:       steps,
	}
	return doc
}

func convertSettings(s v1Settings) *v2Settings {
	out := &v2Settings{
		WorkingDir:     s.WorkingDir,
		MaxConcurrency: s.MaxConcurrency,
	}
	if s.Logging.Level != "" || s.Logging.Pretty {
		out.Logging = &v2Logging{Level: s.Logging.Level, Pretty: s.Logging.Pretty}
	}
	// Drop a wholly-empty settings block so the output stays clean.
	if out.Logging == nil && out.WorkingDir == "" && out.MaxConcurrency == 0 {
		return nil
	}
	return out
}

func marshal(doc *v2Doc) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("encode v2 yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("close yaml encoder: %w", err)
	}
	return buf.Bytes(), nil
}
