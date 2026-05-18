package api

import "net/http"

// deprecatedAPI wraps a handler and stamps every response with RFC 8594
// Deprecation + Sunset hints, plus an X-Asty-New-Path header that
// rewrites the request URL to the new /api/v1 namespace. Operators and
// clients tailing logs notice the headers; UI/CLI release notes drive
// the actual migration. The wrapper is intentionally transparent —
// payloads and status codes pass through.
func deprecatedAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Deprecation", "true")
		w.Header().Set("Sunset", "the legacy /metrics/* data prefix will be removed in the next release; migrate to /api/v1/*")
		newPath := apiPrefix + r.URL.Path
		w.Header().Set("X-Asty-New-Path", newPath)
		next.ServeHTTP(w, r)
	})
}
