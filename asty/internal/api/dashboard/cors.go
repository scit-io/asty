package dashboard

import (
	"net/http"

	"github.com/rs/zerolog/log"
)

// corsOrigin wraps the whole dashboard mux so a browser served from a
// different origin (the SPA in dev, or any cross-origin admin client)
// can reach the REST + SSE surface. Modeled on the gateway's Origin
// middleware:
//
//   - No Origin header (Prometheus scrape, kube /health probe,
//     server-to-server) → pass through untouched, no CORS headers.
//     This keeps /metrics and /health byte-identical for non-browser
//     callers, which is why the wrapper can sit over the whole mux.
//   - Origin present and allowed → reflect it with the CORS headers.
//   - Origin present and denied → 403.
//
// Auth on writes is a bearer token in a header (not a cookie), so we
// advertise the token headers in Allow-Headers and deliberately do NOT
// set Allow-Credentials. Preflight (OPTIONS) is answered here so it
// never reaches the routes.
func (api *API) corsOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		if !api.corsOrigins.Allows(log.Logger, origin) {
			log.Warn().Str("origin", origin).Str("path", r.URL.Path).Msg("dashboard: rejected Origin")
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Expose-Headers", "X-Asty-Leader, X-Request-Id")

		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, X-Asty-Token, Content-Type")
			w.Header().Set("Access-Control-Max-Age", "86400")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
