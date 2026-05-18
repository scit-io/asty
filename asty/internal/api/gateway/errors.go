package gateway

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"strconv"
)

// isTimeoutError reports whether err is a net.Error-style read timeout.
func isTimeoutError(err error) bool {
	if ne, ok := err.(interface{ Timeout() bool }); ok {
		return ne.Timeout()
	}
	return false
}

// natsRequestErrStatus classifies a NATS Request-Reply error into HTTP.
//
//   - 499 "client closed request" — client canceled (closed the conn,
//     server shutdown). Non-standard but widely understood (nginx).
//     The response usually does not reach the client; the code is for
//     logs.
//   - 504 "gateway timeout"       — our own deadline fired
//     (gateway.http.nats_request_timeout); client still connected.
//   - 503 "service unavailable"   — everything else (NATS disconnected,
//     no responders).
func natsRequestErrStatus(clientCtx context.Context, err error) (int, string) {
	// clientCtx distinguishes "client canceled" from "our timeout".
	// WithTimeout(r.Context()) returns DeadlineExceeded in both cases;
	// clientCtx.Err() disambiguates.
	if clientCtx.Err() == context.Canceled {
		return 499, "client closed request"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout, "gateway timeout"
	}
	return http.StatusServiceUnavailable, "service unavailable"
}

// newRequestID returns 16 hex chars (8 random bytes). Used for
// end-to-end tracing: Gateway → NATS header → service log.
func newRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "unknown"
	}
	return fmt.Sprintf("%x", b)
}

// parseStatus parses an HTTP status string into int, or returns 0
// when the value is out of range — caller substitutes a default.
func parseStatus(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 100 || n > 599 {
		return 0
	}
	return n
}
