package logs

import (
	"github.com/nats-io/nats.go"
)

// NATSWriter is a zerolog io.Writer that forwards already-formatted log
// bytes to a NATS subject. TimestampHook in this package injects the
// "timestamp" field before zerolog marshals, so this writer publishes
// p verbatim — no Unmarshal, no Marshal.
type NATSWriter struct {
	nc      *nats.Conn
	subject string
}

// NewNATSWriter creates a writer that publishes to subject on nc. A nil
// nc is tolerated so callers can wire the writer before the NATS
// connection is established.
func NewNATSWriter(nc *nats.Conn, subject string) *NATSWriter {
	return &NATSWriter{nc: nc, subject: subject}
}

// Write implements io.Writer. The Publish error is discarded
// deliberately: this writer sits inside an io.MultiWriter that also
// feeds stderr, and routing the error through zerolog would recurse
// back into this writer. A dropped publish loses the NATS copy of the
// line; the stderr copy still went through.
func (w *NATSWriter) Write(p []byte) (int, error) {
	if w.nc == nil {
		return len(p), nil
	}
	_ = w.nc.Publish(w.subject, p)
	return len(p), nil
}
