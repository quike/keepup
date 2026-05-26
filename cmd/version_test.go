package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionCmd(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		os       string
		arch     string
		sha      string
		wantArch string
		wantOS   string
	}{
		{
			name:     "all fields set",
			version:  "1.0.0",
			os:       runtime.GOOS,
			arch:     runtime.GOARCH,
			sha:      "abc123",
			wantArch: runtime.GOARCH,
			wantOS:   runtime.GOOS,
		},
		{
			name:     "empty arch falls back to runtime",
			version:  "1.0.0",
			os:       runtime.GOOS,
			arch:     "",
			sha:      "abc123",
			wantArch: runtime.GOARCH,
			wantOS:   runtime.GOOS,
		},
		{
			name:     "empty os falls back to runtime",
			version:  "1.0.0",
			os:       "",
			arch:     runtime.GOARCH,
			sha:      "abc123",
			wantArch: runtime.GOARCH,
			wantOS:   runtime.GOOS,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			CLIVersion, CLIOs, CLIArch, CLISha = tc.version, tc.os, tc.arch, tc.sha

			var out bytes.Buffer
			cmd := newVersionCmd()
			cmd.SetOut(&out)
			cmd.SetArgs([]string{})
			require.NoError(t, cmd.Execute())

			var got map[string]string
			require.NoError(t, json.Unmarshal(out.Bytes(), &got))
			assert.Equal(t, tc.version, got["version"])
			assert.Equal(t, tc.wantOS, got["os"])
			assert.Equal(t, tc.wantArch, got["arch"])
			assert.Equal(t, tc.sha, got["sha"])
		})
	}
}

func TestVersionCmd_EncoderErrorPropagates(t *testing.T) {
	// Swap the encoder for one that always fails so the otherwise-unreachable
	// error branch is exercised.
	boom := errors.New("encode-fail")
	orig := versionEncode
	t.Cleanup(func() { versionEncode = orig })
	versionEncode = func(any) ([]byte, error) { return nil, boom }

	var out bytes.Buffer
	cmd := newVersionCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
}
