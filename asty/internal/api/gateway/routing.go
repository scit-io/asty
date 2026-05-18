package gateway

import (
	"net/http"
	"regexp"
	"strings"
)

// validSubjectToken is the whitelist for one path segment under /v1/.
// Each segment becomes part of the NATS subject; "*" and ">" are NATS
// wildcards and spaces/control bytes are invalid subject tokens —
// allowing them would inject into routing.
var validSubjectToken = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// middlewareOrigin validates the Origin header and sets CORS headers.
// Requests without Origin (curl, server-to-server) pass through with
// no CORS headers. Preflight (OPTIONS) is answered here so it never
// reaches rate limiting or routing.
func (gw *Gateway) middlewareOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}

		if !gw.allowedHosts.allows(gw.log, origin) {
			gw.log.Warn().Str("origin", origin).Str("method", r.Method).Str("path", r.URL.Path).Msg("rejected Origin")
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Expose-Headers", "X-Request-Id")
		w.Header().Set("Vary", "Origin")

		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "86400")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// route dispatches {service}/{method...} or {service}/ws. The mux in
// Handler() has already stripped the configured cfg.Prefix (default
// /api/v1), so r.URL.Path starts with the service segment.
func (gw *Gateway) route(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")

	if len(parts) < 1 || parts[0] == "" {
		http.Error(w, "service required", http.StatusBadRequest)
		return
	}

	// Every segment goes into the NATS subject — validate uniformly.
	// Covers both service and methodParts; in the WS branch the trailing
	// "ws" token also matches the regex.
	for _, p := range parts {
		if !validSubjectToken.MatchString(p) {
			http.Error(w, "invalid path segment", http.StatusBadRequest)
			return
		}
	}
	service := parts[0]

	// Every request that reaches this point passed Origin check,
	// rate limit, and path validation — that is the "valid traffic"
	// signal the autoscaler reads via reportRPSLoop. WS handshakes
	// count as one; subsequent frames are not re-counted (under-count
	// for chat-heavy services, acceptable for v1).
	gw.validRequests.Add(1)

	if parts[len(parts)-1] == "ws" {
		// RFC 6455 §4.1: WebSocket handshake requires GET. Reject other
		// methods before wsConnGuard so an invalid method does not
		// consume a slot.
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// contextcheck: WS session lifetime is gw.ctx (server lifecycle),
		// not r.Context() — the latter cancels when the HTTP handler
		// returns; WS must remain open until shutdown or explicit close.
		gw.handleWS(gw.ctx, w, r, service) //nolint:contextcheck
		return
	}

	// {service} without a method segment → subject "api.v1.service."
	// (trailing dot). NATS returns ErrNoResponders and operators see a
	// confusing subject in logs. Reject 400 — clearer than 503.
	if len(parts) < 2 {
		http.Error(w, "method required", http.StatusBadRequest)
		return
	}

	gw.handleHTTP(w, r, service, parts[1:])
}
