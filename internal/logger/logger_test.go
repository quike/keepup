package logger

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewWithWriter_LevelsAndKVs(t *testing.T) {
	tests := []struct {
		name      string
		level     string
		emit      func(l Logger)
		wantSub   string
		notWanted string
	}{
		{
			name:    "info emits at info level",
			level:   "info",
			emit:    func(l Logger) { l.Info("hello", "k", "v") },
			wantSub: `"k":"v"`,
		},
		{
			name:      "debug suppressed at info",
			level:     "info",
			emit:      func(l Logger) { l.Debug("debugging") },
			notWanted: "debugging",
		},
		{
			name:    "invalid level falls back to info, still logs info",
			level:   "not-a-level",
			emit:    func(l Logger) { l.Info("still here") },
			wantSub: "still here",
		},
		{
			name:    "odd-length kv is tolerated",
			level:   "info",
			emit:    func(l Logger) { l.Info("ok", "key-without-val") },
			wantSub: "ok",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			l := NewWithWriter(&buf, tc.level, false)
			tc.emit(l)
			out := buf.String()
			if tc.wantSub != "" {
				assert.True(t, strings.Contains(out, tc.wantSub), "want %q in %q", tc.wantSub, out)
			}
			if tc.notWanted != "" {
				assert.False(t, strings.Contains(out, tc.notWanted), "did not want %q in %q", tc.notWanted, out)
			}
		})
	}
}

func TestNop(t *testing.T) {
	l := Nop()
	// just exercising the surface; should not panic
	l.Debug("x")
	l.Info("x")
	l.Warn("x")
	l.Error("x")
	l.Trace("x")
}
