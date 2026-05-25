// filepath: core/config/config_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewConfig_ValidYAML(t *testing.T) {
	yamlData := `
version: 1

settings:
  logging:
    level: trace
    pretty: true # set to true for human-readable logs

env:
  KEY: "value"

groups:
  - name: program1
    description: "This runs program 1"
    command: "echo $WHATEVER"
    params: ["--flag1", "value1"]

execution:
  - group: ["program1"] # these run concurrently
`
	config, err := NewConfig([]byte(yamlData))
	assert.NoError(t, err)
	assert.Equal(t, 1, config.Version)
	assert.Equal(t, "trace", config.Settings.Logging.Level)
	assert.Equal(t, "program1", config.Groups[0].Name)
	assert.Equal(t, "value", config.Env["KEY"])
}

func TestNewConfig_InvalidYAML(t *testing.T) {
	yamlData := `
invalid_yaml: [`
	_, err := NewConfig([]byte(yamlData))
	assert.Error(t, err)
}

func TestLoadConfig_ValidFile(t *testing.T) {
	yamlData := `
version: 1

settings:
  logging:
    level: info
    pretty: true # set to true for human-readable logs

env:
  KEY: "value"

groups:
  - name: group1
    description: "This runs program 1"
    command: "echo $WHATEVER"
    params: ["--flag1", "value1"]

execution:
  - group: ["group1"] # these run concurrently
`
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	assert.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.Write([]byte(yamlData))
	assert.NoError(t, err)
	tmpFile.Close()

	config, err := LoadConfig(tmpFile.Name())
	assert.NoError(t, err)
	assert.Equal(t, 1, config.Version)
	assert.Equal(t, "info", config.Settings.Logging.Level)
	assert.Equal(t, "group1", config.Groups[0].Name)
	assert.Equal(t, "value", config.Env["KEY"])
}

func TestLoadConfig_ValidResourceFile(t *testing.T) {
	configPath := "./test-resources/config-valid.yml"

	config, err := LoadConfig(configPath)
	assert.NoError(t, err)
	assert.Equal(t, 1, config.Version)
	assert.Equal(t, "debug", config.Settings.Logging.Level)
	assert.Equal(t, 3, len(config.Groups))
	assert.Equal(t, "global-env", config.Groups[0].Name)
	assert.Equal(t, "scoped-env", config.Groups[1].Name)
	assert.Equal(t, "combined-responses", config.Groups[2].Name)
	assert.Equal(t, "global value", config.Env["WHATEVER"])
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("nonexistent.yaml")
	assert.Error(t, err)
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	assert.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.Write([]byte("invalid_yaml: ["))
	assert.NoError(t, err)
	tmpFile.Close()

	_, err = LoadConfig(tmpFile.Name())
	assert.Error(t, err)
}

func TestLoadConfig_InvalidResourceFile(t *testing.T) {
	configPath := "./test-resources/config-invalid.yml"

	_, err := LoadConfig(configPath)
	assert.Error(t, err)
}

func TestLoadConfig_ExpandHomeDir(t *testing.T) {
	yamlData := `
version: 1

settings:
  logging:
    level: info
    pretty: true # set to true for human-readable logs
  working-dir: /tmp # optional default directory to run commands
  max-concurrency: 2 # max 2 parallel programs at any time
`
	homeDir, err := os.UserHomeDir()
	assert.NoError(t, err)

	tmpFile := filepath.Join(homeDir, "config-test.yaml")
	err = os.WriteFile(tmpFile, []byte(yamlData), 0644)
	assert.NoError(t, err)
	defer os.Remove(tmpFile)

	config, err := LoadConfig("~/config-test.yaml")
	assert.NoError(t, err)
	assert.Equal(t, 1, config.Version)
	assert.Equal(t, "info", config.Settings.Logging.Level)
	assert.Equal(t, "/tmp", config.Settings.WorkingDir)
}

func TestNewConfig_EmptyYAML(t *testing.T) {
	config, err := NewConfig([]byte(""))
	assert.NoError(t, err)
	assert.Equal(t, 0, config.Version)
	assert.Nil(t, config.Groups)
	assert.Nil(t, config.Execution)
	assert.Nil(t, config.Env)
}

func TestLoadConfig_EmptyFile(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	assert.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	tmpFile.Close()

	config, err := LoadConfig(tmpFile.Name())
	assert.NoError(t, err)
	assert.Equal(t, 0, config.Version)
	assert.Nil(t, config.Groups)
	assert.Nil(t, config.Execution)
	assert.Nil(t, config.Env)
}
