package log

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

var Logger zerolog.Logger

func Init(humanReadable bool) {
	level := parseLevel(os.Getenv("LOG_LEVEL"))
	zerolog.SetGlobalLevel(level)

	if humanReadable {
		output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
		Logger = zerolog.New(output).With().Timestamp().Logger()
	} else {
		Logger = zerolog.New(os.Stdout).With().Timestamp().Logger()
	}

	zerolog.DefaultContextLogger = &Logger
}

func parseLevel(s string) zerolog.Level {
	switch s {
	case "trace":
		return zerolog.TraceLevel
	case "debug":
		return zerolog.DebugLevel
	case "warn":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}
