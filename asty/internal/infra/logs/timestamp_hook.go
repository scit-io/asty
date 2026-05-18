package logs

import (
	"time"

	"github.com/rs/zerolog"
)

// TimestampHook adds a "timestamp" field (Unix seconds) to every
// zerolog event before marshal. Downstream consumers — UI log view,
// in-memory log buffer, SSE stream — read entry["timestamp"] alongside
// zerolog's native "time" field; the hook keeps the field present so
// NATSWriter can publish bytes verbatim instead of round-tripping JSON
// to inject the field per line.
type TimestampHook struct{}

// Run is called by zerolog for every log event before the event is
// marshalled. e.Int64 appends to the in-progress buffer, so the field
// lands in the same JSON object every reader will see.
func (TimestampHook) Run(e *zerolog.Event, _ zerolog.Level, _ string) {
	e.Int64("timestamp", time.Now().Unix())
}
