package api

import (
	"net/http"
	"strings"
)

// wantsSSE reports whether the client asked for a Server-Sent Events
// stream via the Accept header. Used as the content-negotiation key
// on GET routes that can serve either a one-shot JSON snapshot or a
// live SSE feed of the same resource.
//
// Substring match is intentional — there is exactly one media type
// that matters and full RFC 7231 q-value parsing would be overkill.
func wantsSSE(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}
