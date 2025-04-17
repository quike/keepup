package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Version   int               `yaml:"version"`
	Settings  Settings          `yaml:"settings"`
	Groups    []Group           `yaml:"groups"`
	Execution []Step            `yaml:"execution"`
	Env       map[string]string `yaml:"env"`
}

type Logging struct {
	Level  string `yaml:"level"`
	Pretty bool   `yaml:"pretty"`
}

type Settings struct {
	dryRun     bool    `yaml:"dry-un"`
	Logging    Logging `yaml:"logging"`
	WorkingDir string  `yaml:"working-dir"`
}

type Group struct {
	Name        string            `yaml:"name"`
	Command     string            `yaml:"command"`
	Params      []string          `yaml:"params"`
	Shell       string            `yaml:"shell,omitempty"` // optional, specific shell
	Description string            `yaml:"description,omitempty"`
	Concurrency any               `yaml:"concurrency,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
}

type Step struct {
	Group []string `yaml:"group"` // List of group names to run in parallel
}

func NewConfig(b []byte) (*Config, error) {
	var config Config
	if err := yaml.Unmarshal(b, &config); err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal configuration")
	}
	return &config, nil
}

func LoadConfig(path string) (*Config, error) {
	// Expand ~ to home dir
	if path[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cannot determine home dir: %w", err)
		}
		path = filepath.Join(home, path[2:])
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("invalid yaml format: %w", err)
	}

	return &config, nil
}
