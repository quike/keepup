package logger

import (
	"bytes"
	"os"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestNewLogger_DefaultLevel(t *testing.T) {
	// Create a pipe to capture the log output
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Failed to create pipe: %v", err)
	}
	// Backup the original os.Stdout
	originalStdout := os.Stdout
	// Redirect os.Stdout to the pipe
	os.Stdout = w
	// Ensure that we restore the original os.Stdout after the test
	defer func() { os.Stdout = originalStdout }()

	// Create the logger with the default level
	log := NewLogger("", false)

	// Log something at the "info" level
	log.Info().Msg("default level test")

	// Close the writer to finish writing to the pipe
	w.Close()

	// Read the output from the reader
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	// Assertions
	assert.Contains(t, buf.String(), "default level test")
}

func TestNewLogger_PrettyFormat(t *testing.T) {
	// Create a pipe to capture the log output
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Failed to create pipe: %v", err)
	}
	// Backup the original os.Stdout
	originalStdout := os.Stdout
	// Redirect os.Stdout to the pipe
	os.Stdout = w
	// Ensure that we restore the original os.Stdout after the test
	defer func() { os.Stdout = originalStdout }()

	// Create the logger on info level with "pretty" output enabled
	level := "info"
	pretty := true
	log := NewLogger(level, pretty)

	// Log something with pretty format enabled
	log.Info().Msg("pretty format test")

	// Close the writer to finish writing to the pipe
	w.Close()

	// Read the output from the reader
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	/// Assertions
	output := buf.String()
	assert.Contains(t, output, "pretty format test")
	assert.False(t, output[0] == '{', "Expected pretty output, got JSON")
}

func TestNewLogger_ValidLogLevel(t *testing.T) {
	// Create a pipe to capture the log output
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Failed to create pipe: %v", err)
	}
	// Backup the original os.Stdout
	originalStdout := os.Stdout
	// Redirect os.Stdout to the pipe
	os.Stdout = w
	// Ensure that we restore the original os.Stdout after the test
	defer func() { os.Stdout = originalStdout }()

	// Create the logger with "debug" level
	log := NewLogger("debug", false)

	// Ensure the logger is set to "debug" level
	assert.Equal(t, zerolog.DebugLevel, log.GetLevel())

	// Log something at the "debug" level
	log.Debug().Msg("valid level test")

	// Close the writer to finish writing to the pipe
	w.Close()

	// Read the output from the reader
	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	if err != nil {
		t.Fatalf("Failed to read from pipe: %v", err)
	}

	/// Assertions
	assert.Contains(t, buf.String(), "valid level test")
}

func TestNewLogger_InvalidLogLevel(t *testing.T) {
	// Create a pipe to capture the log output
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Failed to create pipe: %v", err)
	}
	// Backup the original os.Stdout
	originalStdout := os.Stdout
	// Redirect os.Stdout to the pipe
	os.Stdout = w
	// Ensure that we restore the original os.Stdout after the test
	defer func() { os.Stdout = originalStdout }()

	// Create the logger with an invalid log level
	log := NewLogger("badlevel", false)

	// Ensure that the level defaults to "trace"
	assert.Equal(t, zerolog.TraceLevel, log.GetLevel()) // fallback to info level

	// Log something at the "warn" level
	log.Warn().Msg("whatever")

	// Close the writer to finish writing to the pipe
	w.Close()

	// Read the output from the reader
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	// Check that the output contains the warning message
	output := buf.String()
	assert.Contains(t, output, "Invalid log level")
	assert.Contains(t, output, "whatever")
}

func TestGetLogger_ReturnsPointer(t *testing.T) {
	// Assign the logger to the global Log variable
	Log = NewLogger("info", false)

	// Verify that GetLogger returns the pointer to the global logger
	ptr := GetLogger()
	assert.Same(t, &Log, ptr)
}
