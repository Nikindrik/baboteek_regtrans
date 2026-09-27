package logger

import (
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

func New(levelStr string, isDevelopment bool) zerolog.Logger {
	var level zerolog.Level
	switch strings.ToLower(levelStr) {
	case "debug":
		level = zerolog.DebugLevel
	case "info":
		level = zerolog.InfoLevel
	case "warn":
		level = zerolog.WarnLevel
	case "error":
		level = zerolog.ErrorLevel
	default:
		level = zerolog.InfoLevel
	}

	zerolog.SetGlobalLevel(level)
	zerolog.TimeFieldFormat = time.RFC3339Nano

	if isDevelopment {
		output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05.000"}
		return zerolog.New(output).With().Timestamp().Caller().Logger()
	}

	return zerolog.New(os.Stdout).With().Timestamp().Logger()
}
