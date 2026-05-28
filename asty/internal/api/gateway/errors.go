package gateway

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/nats-io/nats.go"
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

// ADR-32 service-error headers — wire-protocol identifiers shared with
// nats.go/micro.ErrorHeader / micro.ErrorCodeHeader. Not imported from
// micro because the gateway is a NATS client, not a service; micro is
// the service-side helper package and pulling it in just for two string
// literals would be misleading.
// https://github.com/nats-io/nats-architecture-and-design/blob/main/adr/ADR-32.md
const (
	natsServiceErrorCode = "Nats-Service-Error-Code"
	natsServiceError     = "Nats-Service-Error"
)

// readServiceError extracts an ADR-32 service-error from a NATS reply.
// Returns (0, "") when no error was signaled or the code is unparseable.
// The spec ("safe to parse as a number") leaves the code range open;
// callers map it to their own domain (HTTP status, WS close code).
func readServiceError(h nats.Header) (code int, description string) {
	raw := h.Get(natsServiceErrorCode)
	if raw == "" {
		return 0, ""
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, ""
	}
	return n, h.Get(natsServiceError)
}
