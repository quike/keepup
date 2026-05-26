// Package logger defines a small Logger interface for the domain and a
// zerolog-backed implementation for the CLI.
package logger

import (
	"io"
	"os"

	"github.com/rs/zerolog"
)

// Logger is the minimal interface the domain needs. Implementations are
// expected to be safe for concurrent use.
type Logger interface {
	Debug(msg string, kv ...any)
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
	Error(msg string, kv ...any)
	Trace(msg string, kv ...any)
}

// New returns a zerolog-backed Logger writing to stdout.
// level is parsed by zerolog; an empty or invalid value falls back to info.
func New(level string, pretty bool) Logger {
	return NewWithWriter(os.Stdout, level, pretty)
}

// NewWithWriter is like New but writes to the provided writer; intended for tests.
func NewWithWriter(w io.Writer, level string, pretty bool) Logger {
	out := w
	if pretty {
		out = zerolog.ConsoleWriter{Out: w}
	}
	zl := zerolog.New(out).With().Timestamp().Logger()
	if parsed, err := zerolog.ParseLevel(level); err == nil && level != "" {
		zl = zl.Level(parsed)
	} else {
		zl = zl.Level(zerolog.InfoLevel)
	}
	return &zerologAdapter{zl: zl}
}

// Nop returns a Logger that discards all messages. Useful for tests.
func Nop() Logger { return nopLogger{} }

type zerologAdapter struct{ zl zerolog.Logger }

func (z *zerologAdapter) Debug(msg string, kv ...any) { z.emit(z.zl.Debug(), msg, kv) }
func (z *zerologAdapter) Info(msg string, kv ...any)  { z.emit(z.zl.Info(), msg, kv) }
func (z *zerologAdapter) Warn(msg string, kv ...any)  { z.emit(z.zl.Warn(), msg, kv) }
func (z *zerologAdapter) Error(msg string, kv ...any) { z.emit(z.zl.Error(), msg, kv) }
func (z *zerologAdapter) Trace(msg string, kv ...any) { z.emit(z.zl.Trace(), msg, kv) }

// emit attaches kv pairs (as alternating key/value) and writes the event.
// Odd-length kv slices are tolerated by appending a missing-value marker.
func (z *zerologAdapter) emit(ev *zerolog.Event, msg string, kv []any) {
	for i := 0; i < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			continue
		}
		var v any
		if i+1 < len(kv) {
			v = kv[i+1]
		}
		ev = ev.Interface(k, v)
	}
	ev.Msg(msg)
}

type nopLogger struct{}

func (nopLogger) Debug(string, ...any) {}
func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}
func (nopLogger) Trace(string, ...any) {}
