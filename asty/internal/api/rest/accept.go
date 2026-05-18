package rest

import (
	"net/http"
	"strings"
)

// transportSSE reports whether the client asked for a streaming feed
// over Server-Sent Events (Accept: text/event-stream). Used as the
// content-negotiation key on GET routes that can serve either a live
// stream or a one-shot polled response.
//
// Substring match is intentional — there is exactly one media type
// that matters and full RFC 7231 q-value parsing would be overkill.
func transportSSE(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

// transportPolling reports whether the client wants a one-shot
// response (JSON snapshot by default, Prometheus text when the
// Accept header asks for it). It is the complement of transportSSE
// — kept as its own function so the polling path reads explicitly
// at every call site, and so future polling-format detection
// (Prometheus vs JSON vs other) attaches here without touching the
// transport check.
func transportPolling(r *http.Request) bool {
	return !transportSSE(r)
}
