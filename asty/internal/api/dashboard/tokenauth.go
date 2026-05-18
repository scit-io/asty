package dashboard

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// tokenAuth gates write handlers behind cfg.Token. Returns 401 on a
// missing or mismatched token, 503 if the server has no token
// configured (dev_mode permits an empty cfg.Token; in that case the
// middleware passes everything through unconditionally, matching the
// rest of dev convenience).
//
// Header conventions supported (any of):
//   - Authorization: Bearer <token>
//   - X-Asty-Token: <token>
//
// Constant-time comparison avoids the timing oracle that would otherwise
// leak token length / prefix. Token reads from cfg per request so a
// future hot-reload of cfg.Token (not yet implemented) takes effect
// immediately.
func (api *API) tokenAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		expected := api.ctx.Config().Token
		if expected == "" {
			// dev_mode or unconfigured: writes are not gated by token.
			h(w, r)
			return
		}
		got := extractToken(r)
		if got == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="asty"`)
			api.writeError(w, http.StatusUnauthorized, "missing token", nil)
			return
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
			api.writeError(w, http.StatusUnauthorized, "invalid token", nil)
			return
		}
		h(w, r)
	}
}

// extractToken pulls the bearer token from Authorization or the
// X-Asty-Token header. Whitespace is trimmed; an empty Authorization
// value falls through to X-Asty-Token so clients can choose either
// convention without coordination.
func extractToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		if strings.HasPrefix(auth, "Bearer ") {
			return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		}
	}
	return strings.TrimSpace(r.Header.Get("X-Asty-Token"))
}
