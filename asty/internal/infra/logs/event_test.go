package logs

import (
	"encoding/json"
	"testing"
)

func TestParseEvent_Structured(t *testing.T) {
	raw := []byte(`{"level":"warn","time":1747853533,"timestamp":1747853533,"component":"discovery","message":"failed to discover nodes","error":"failed to resolve dev.local: no such host","attempts":3}`)
	e, err := ParseEvent(raw)
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	if e.Level != "warn" || e.Component != "discovery" || e.Message != "failed to discover nodes" {
		t.Fatalf("structured fields not pulled: %+v", e)
	}
	if e.Err != "failed to resolve dev.local: no such host" {
		t.Fatalf("error field not pulled: %q", e.Err)
	}
	if e.Timestamp != 1747853533 || e.Time != 1747853533 {
		t.Fatalf("times not pulled: %+v", e)
	}
	if e.Fields["attempts"] != float64(3) {
		t.Fatalf("extra fields not pulled: %+v", e.Fields)
	}
	if e.IsLine() {
		t.Fatalf("structured event misclassified as raw line")
	}
}

func TestParseEvent_RawStdout(t *testing.T) {
	raw := []byte(`{"line":"hello world","timestamp":1747853533}`)
	e, err := ParseEvent(raw)
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	if !e.IsLine() {
		t.Fatalf("raw stdout misclassified as structured: %+v", e)
	}
	if e.Line != "hello world" || e.Timestamp != 1747853533 {
		t.Fatalf("line/timestamp not pulled: %+v", e)
	}
}

func TestParseEvent_FallbackTimestamp(t *testing.T) {
	raw := []byte(`{"level":"info","time":1234,"message":"hi"}`)
	e, _ := ParseEvent(raw)
	if e.Timestamp != 1234 {
		t.Fatalf("timestamp should fall back to time: %+v", e)
	}
}

func TestParseEvent_RoundtripNoDoubleFields(t *testing.T) {
	// Demo emits flat zerolog JSON.
	raw := []byte(`{"level":"info","time":1,"component":"xws","message":"started","inactivity_timeout":180000}`)
	first, err := ParseEvent(raw)
	if err != nil {
		t.Fatalf("first ParseEvent: %v", err)
	}
	if first.Fields["inactivity_timeout"] != float64(180000) {
		t.Fatalf("first parse lost extra field: %+v", first.Fields)
	}

	// Agent re-marshals into wire shape with a nested "fields" object.
	second, err := ParseEvent(first.MarshalWire())
	if err != nil {
		t.Fatalf("second ParseEvent: %v", err)
	}
	if _, doubled := second.Fields["fields"]; doubled {
		t.Fatalf("fields container nested under itself: %+v", second.Fields)
	}
	if second.Fields["inactivity_timeout"] != float64(180000) {
		t.Fatalf("roundtrip lost the actual field: %+v", second.Fields)
	}
}

func TestEvent_MarshalWire(t *testing.T) {
	e := Event{Level: "info", Component: "server", Message: "ok", Timestamp: 7}
	out := e.MarshalWire()
	var back map[string]any
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("MarshalWire produced invalid JSON: %v\n%s", err, out)
	}
	if back["level"] != "info" || back["component"] != "server" || back["message"] != "ok" {
		t.Fatalf("MarshalWire dropped fields: %v", back)
	}
}
