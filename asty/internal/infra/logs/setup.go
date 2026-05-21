package logs

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// InitGlobal configures the process-wide zerolog logger:
//
//   - Unix-second time field so wire JSON travels as a small int64 and
//     the dashboard renders relative time client-side.
//   - Pretty ConsoleWriter for stderr with colored levels and the
//     "component=<x>" chip rendered inline for human eyes.
//   - TimestampHook stamps the duplicate "timestamp" key the SSE
//     pipeline reads (kept beside zerolog's native "time" for legacy
//     consumers).
//   - .With().Str("component", …) plants the component on every
//     event without each call site having to think about it.
//
// Call once from main() before any logging happens.
func InitGlobal(component string) {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = zerolog.New(consoleWriter()).
		Hook(TimestampHook{}).
		With().
		Timestamp().
		Str("component", component).
		Logger()
}

// SetLevel switches the global zerolog level from a Config.LogLevel
// string. Unknown values are no-ops — zerolog's default (info) sticks.
// Called from main() once config has been loaded so the env override
// stays inside the core/config layer.
func SetLevel(level string) {
	switch strings.ToLower(level) {
	case "trace":
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "warn", "warning":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	case "fatal":
		zerolog.SetGlobalLevel(zerolog.FatalLevel)
	}
}

// AttachNATS reroutes the global logger so every event also lands on
// `subject` as JSON bytes. The stderr ConsoleWriter is preserved
// alongside the NATS publisher via io.MultiWriter, so operators
// keep their tail and the dashboard gets its feed from one source of
// truth.
func AttachNATS(nc *nats.Conn, subject string) {
	natsWriter := NewNATSWriter(nc, subject)
	log.Logger = log.Logger.Output(io.MultiWriter(consoleWriter(), natsWriter))
}

// For returns a child logger annotated with a sub-component tag.
// Subsystems that own a logger field (gateway, demo handlers) take it
// at construction; ad-hoc users can call this inline.
func For(component string) zerolog.Logger {
	return log.With().Str("component", component).Logger()
}

// consoleWriter centralises the stderr formatter so InitGlobal and
// AttachNATS produce identical pretty output. Color emission is
// gated on the stderr-is-a-terminal check so piped output stays
// ANSI-free.
func consoleWriter() zerolog.ConsoleWriter {
	return zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.RFC3339,
		NoColor:    !isStderrTTY(),
	}
}

// isStderrTTY detects whether stderr is hooked to a terminal so colored
// output is only emitted in interactive sessions. systemd, journald,
// piped collectors get plain text.
func isStderrTTY() bool {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
