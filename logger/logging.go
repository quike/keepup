package logger

import (
	"io"
	"os"

	"github.com/rs/zerolog"
)

var Log zerolog.Logger

func NewLogger(level string, pretty bool) zerolog.Logger {
	out := io.Writer(os.Stdout)
	if pretty {
		out = zerolog.ConsoleWriter{Out: out}
	}

	log := zerolog.New(out).With().Timestamp().Logger()
	if level == "" {
		return log
	}

	parsedLevel, err := zerolog.ParseLevel(level)
	if err != nil {
		log.Warn().Msgf("Invalid log level %q, using the default level instead", level)
		return log
	}
	zerolog.DefaultContextLogger = &log

	return log.Level(parsedLevel)
}

func GetLogger() *zerolog.Logger {
	return &Log
}
