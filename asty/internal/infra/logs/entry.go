package logs

import (
	"encoding/json"
	"fmt"
)

// LineFrame is the {"line","timestamp"} JSON object exchanged on every
// log SSE path. The agent's process-log streamer wraps each stdout
// line in this shape (asty.v1.agent.<id>.logs.<svc>), and SSE handlers
// emit it to the browser. Field names match the UI type at
// asty/web/src/types/index.ts.
type LineFrame struct {
	Line      string `json:"line"`
	Timestamp int64  `json:"timestamp"`
}

// ZerologEntry is the parsed shape of a zerolog event published to
// asty.v1.server.logs or asty.v1.agent.<id>.logs.agent. Known fields
// are pulled into typed slots; everything else lands in Extras so the
// display can render structured context without losing it.
//
// Time is decoded as int64 because cmd/main.go sets TimeFieldFormat to
// TimeFormatUnix — zerolog writes "time" as a numeric Unix second. In
// practice the system reads Timestamp (always present via
// TimestampHook); Time is kept here for completeness.
type ZerologEntry struct {
	Level     string
	Message   string
	Time      int64
	Timestamp int64
	Extras    map[string]json.RawMessage
}

// zerologKnownFields are the keys DecodeZerologEntry pulls into typed
// slots; everything else goes into Extras.
var zerologKnownFields = []string{"level", "message", "time", "timestamp"}

// DecodeZerologEntry parses data as a zerolog JSON event. Known fields
// populate typed slots; remaining fields are kept as raw JSON in
// Extras for downstream rendering.
func DecodeZerologEntry(data []byte) (*ZerologEntry, error) {
	raw := make(map[string]json.RawMessage)
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode zerolog entry: %w", err)
	}
	e := &ZerologEntry{}
	if v, ok := raw["level"]; ok {
		_ = json.Unmarshal(v, &e.Level)
	}
	if v, ok := raw["message"]; ok {
		_ = json.Unmarshal(v, &e.Message)
	}
	if v, ok := raw["time"]; ok {
		_ = json.Unmarshal(v, &e.Time)
	}
	if v, ok := raw["timestamp"]; ok {
		_ = json.Unmarshal(v, &e.Timestamp)
	}
	for _, k := range zerologKnownFields {
		delete(raw, k)
	}
	if len(raw) > 0 {
		e.Extras = raw
	}
	return e, nil
}

// AsLineFrame reports whether the entry is actually a logstream-wrapped
// LineFrame (no zerolog metadata, just a "line" key) and returns the
// unwrapped value. SSE and log-buffer paths use this to render
// process-stdout lines as plain text instead of zerolog's
// "[time] [level] message" template with the stdout line stuffed into
// extras.
func (e *ZerologEntry) AsLineFrame() (LineFrame, bool) {
	if e.Level != "" || e.Message != "" || len(e.Extras) != 1 {
		return LineFrame{}, false
	}
	raw, ok := e.Extras["line"]
	if !ok {
		return LineFrame{}, false
	}
	var line string
	if json.Unmarshal(raw, &line) != nil {
		return LineFrame{}, false
	}
	return LineFrame{Line: line, Timestamp: e.Timestamp}, true
}
