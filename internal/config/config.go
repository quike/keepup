// Package config defines the YAML wire format for keepup and loads/validates it.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the top-level keepup configuration document.
type Config struct {
	Version   int               `yaml:"version"`
	Settings  Settings          `yaml:"settings"`
	Groups    []Group           `yaml:"groups"`
	Execution []Step            `yaml:"execution"`
	Env       map[string]string `yaml:"env"`
}

// Logging configures the keepup logger.
type Logging struct {
	Level  string `yaml:"level"`
	Pretty bool   `yaml:"pretty"`
}

// Settings holds global runtime settings.
type Settings struct {
	DryRun         bool    `yaml:"dry-run"`
	Logging        Logging `yaml:"logging"`
	WorkingDir     string  `yaml:"working-dir"`
	MaxConcurrency int     `yaml:"max-concurrency"`
}

// Group is a named command unit declared under `groups:`.
//
// Shell controls how the command is launched:
//   - empty: exec directly with Params as argv (safe; no shell interpretation)
//   - non-empty: pipe `command + params` through the named shell program (opt-in).
type Group struct {
	Name        string            `yaml:"name"`
	Command     string            `yaml:"command"`
	Params      []string          `yaml:"params"`
	Shell       string            `yaml:"shell,omitempty"`
	Description string            `yaml:"description,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
}

// UseShell reports whether the group opted into shell mode.
func (g *Group) UseShell() bool { return g.Shell != "" }

// Step is one execution step; the listed groups run concurrently.
type Step struct {
	Group []string `yaml:"group"`
}

// NewConfig parses YAML bytes into a Config.
func NewConfig(b []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal configuration: %w", err)
	}
	return &cfg, nil
}

// LoadConfig reads a YAML config from disk. Supports a leading "~/" home expansion.
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
