package logs

import (
	"encoding/json"
	"time"

	"github.com/nats-io/nats.go"
)

// NATSWriter is a zerolog writer that publishes to NATS
type NATSWriter struct {
	nc      *nats.Conn
	subject string
}

// NewNATSWriter creates a new NATS writer for zerolog
func NewNATSWriter(nc *nats.Conn, subject string) *NATSWriter {
	return &NATSWriter{
		nc:      nc,
		subject: subject,
	}
}

// Write implements io.Writer interface
func (w *NATSWriter) Write(p []byte) (n int, err error) {
	if w.nc == nil {
		return len(p), nil
	}

	var entry map[string]interface{}
	if err := json.Unmarshal(p, &entry); err != nil {
		return len(p), nil
	}

	if _, ok := entry["timestamp"]; !ok {
		entry["timestamp"] = time.Now().Unix()
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return len(p), nil
	}

	w.nc.Publish(w.subject, data)
	return len(p), nil
}
