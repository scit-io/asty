package logs

import (
	"encoding/json"
)

// Event is the canonical structured log shape that flows through Asty:
// zerolog emits it as JSON, NATSWriter publishes the bytes verbatim,
// the in-memory buffer stores it, the dashboard SSE handlers ship it to
// the browser, and the web UI renders it with colored level/component
// tags. Keep field names in lockstep with the LogEvent type in
// asty/web/src/types/index.ts.
type Event struct {
	Time      int64           `json:"time,omitempty"`      // zerolog "time" (Unix seconds).
	Timestamp int64           `json:"timestamp,omitempty"` // redundant copy added by TimestampHook.
	Level     string          `json:"level,omitempty"`
	Component string          `json:"component,omitempty"` // server | agent | gateway | drainer | …
	Message   string          `json:"message,omitempty"`
	Err       string          `json:"error,omitempty"` // zerolog .Err() default key.
	Fields    map[string]any  `json:"fields,omitempty"`
	Line      string          `json:"line,omitempty"` // raw stdout from a managed process.
}

// builtinKeys are the zerolog/Asty keys ParseEvent pulls into typed
// slots. "fields" sits here too: an inbound payload that already
// carries a "fields" object (re-parse of our own wire format) must
// not let that container nest under itself. ParseEvent lifts it
// directly into Event.Fields; the generic non-builtin loop below
// then skips the same key so the demo's flat shape and the wire's
// nested shape both end up as a single, un-doubled Fields map.
var builtinKeys = map[string]struct{}{
	"time": {}, "timestamp": {}, "level": {}, "component": {},
	"message": {}, "error": {}, "line": {}, "fields": {},
}

// ParseEvent decodes a single line of JSON into Event. Raw stdout lines
// wrapped as {"line": "...", "timestamp": N} land in the same shape —
// Level/Component stay empty, Line carries the text.
func ParseEvent(data []byte) (*Event, error) {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	e := &Event{}
	unmarshalInto(raw, "time", &e.Time)
	unmarshalInto(raw, "timestamp", &e.Timestamp)
	unmarshalInto(raw, "level", &e.Level)
	unmarshalInto(raw, "component", &e.Component)
	unmarshalInto(raw, "message", &e.Message)
	unmarshalInto(raw, "error", &e.Err)
	unmarshalInto(raw, "line", &e.Line)
	// If the payload already carries a "fields" container (the wire
	// shape of our own MarshalWire), unwrap it first so the loop
	// below doesn't double-nest the same keys.
	if v, ok := raw["fields"]; ok {
		var inner map[string]any
		if err := json.Unmarshal(v, &inner); err == nil && len(inner) > 0 {
			e.Fields = inner
		}
	}
	// Pull anything we don't have a typed slot for into Fields.
	for k, v := range raw {
		if _, builtin := builtinKeys[k]; builtin {
			continue
		}
		if e.Fields == nil {
			e.Fields = map[string]any{}
		}
		var any any
		if err := json.Unmarshal(v, &any); err == nil {
			e.Fields[k] = any
		}
	}
	// The on-the-wire payload favours "timestamp"; fall back to "time"
	// when only the zerolog default is present so the UI always has a
	// monotonic anchor to sort by.
	if e.Timestamp == 0 && e.Time != 0 {
		e.Timestamp = e.Time
	}
	return e, nil
}

// IsLine reports whether the event is a raw stdout line wrapped for
// transport — Line is set, but the structured Level/Message slots are
// empty. Component may carry the service tag so the dashboard can
// still color the row.
func (e *Event) IsLine() bool {
	return e.Level == "" && e.Message == "" && e.Line != ""
}

// MarshalWire encodes the event in the same JSON shape SSE consumers
// see. The shape is stable across the producer (zerolog), the buffer,
// and the wire — one path, one renderer.
func (e *Event) MarshalWire() []byte {
	b, _ := json.Marshal(e)
	return b
}

// unmarshalInto silently ignores decode errors. Unknown shape just
// means the field stays empty — callers fall back to whatever Fields
// holds.
func unmarshalInto(raw map[string]json.RawMessage, key string, dst any) {
	v, ok := raw[key]
	if !ok {
		return
	}
	_ = json.Unmarshal(v, dst)
}
