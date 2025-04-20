package app

import (
	"testing"

	"github.com/quike/keepup/core/config"
	"github.com/stretchr/testify/assert"
)

func TestExecutor_Run_Success(t *testing.T) {
	// Mock configuration
	cfg := config.Config{
		Groups: []config.Group{
			{Name: "group1", Command: "echo", Params: []string{"Hello"}},
			{Name: "group2", Command: "echo", Params: []string{"World"}},
		},
		Execution: []config.Step{
			{Group: []string{"group1", "group2"}},
		},
	}

	executor := NewExecutor(cfg)

	// Run the executor
	err := executor.Run()

	// Assertions
	assert.NoError(t, err)
	value, _ := executor.Outputs.Load("group1")
	assert.Equal(t, "Hello\n", value)
	value, _ = executor.Outputs.Load("group2")
	assert.Equal(t, "World\n", value)
}

func TestExecutor_Run_GroupNotDefined(t *testing.T) {
	// Mock configuration
	cfg := config.Config{
		Groups: []config.Group{
			{Name: "group1", Command: "echo", Params: []string{"Hello"}},
		},
		Execution: []config.Step{
			{Group: []string{"group1", "group2"}}, // group2 is not defined
		},
	}

	executor := NewExecutor(cfg)

	// Run the executor
	err := executor.Run()

	// Assertions
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "group group2 not defined")
}

func TestExecutor_Run_CommandFails(t *testing.T) {
	// Mock configuration
	cfg := config.Config{
		Groups: []config.Group{
			{Name: "group1", Command: "false", Params: []string{}}, // Command that always fails
		},
		Execution: []config.Step{
			{Group: []string{"group1"}},
		},
	}

	executor := NewExecutor(cfg)

	// Run the executor
	err := executor.Run()

	// Assertions
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "step 1 failed")
}

func TestExecutor_ExpandParams(t *testing.T) {
	// Mock configuration
	cfg := config.Config{
		Groups: []config.Group{
			{Name: "group1", Command: "echo", Params: []string{"{{ output.group2 }}"}},
			{Name: "group2", Command: "echo", Params: []string{"World"}},
		},
		Execution: []config.Step{
			{Group: []string{"group2", "group1"}},
		},
	}

	executor := NewExecutor(cfg)

	// Run the executor
	err := executor.Run()

	// Assertions
	assert.NoError(t, err)
	value, _ := executor.Outputs.Load("group2")
	assert.Equal(t, "World\n", value)
	value, _ = executor.Outputs.Load("group1")
	assert.Equal(t, "{{ output.group2 }}\n", value)
}
