package gateway

import (
	"net/http"

	"asty/asty/internal/core/netutil"
)

// middlewareRateLimit applies per-IP rate limiting to /v1/ routes.
//
// Check order:
//  1. If the request path matches a service-declared rate_limit rule
//     (longest prefix wins), that rule's per-IP cap is checked first.
//  2. Every /v1/ route is then checked against the general cap.
//
// /health bypasses this chain entirely.
func (gw *Gateway) middlewareRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := netutil.RealIP(r, gw.cfg.RateLimit.TrustedProxy)

		if allowed, prefix := gw.rl.allowPath(ip, r.URL.Path); prefix != "" {
			if !allowed {
				gw.log.Warn().Str("ip", ip).Str("path", r.URL.Path).Str("rule", prefix).Msg("path rate limit exceeded")
				writeRateLimited(w)
				return
			}
		}

		if !gw.rl.allow(ip) {
			gw.log.Warn().Str("ip", ip).Str("path", r.URL.Path).Msg("rate limit exceeded")
			writeRateLimited(w)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func writeRateLimited(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", "1")
	http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
}

// wsConnGuard checks the global WS connection cap and bumps the counter.
// Returns (false, nil) when the cap is reached — caller must answer 503.
// On (true, release), caller must invoke release() once the WS closes.
func (gw *Gateway) wsConnGuard() (bool, func()) {
	if gw.rl.wsConns.Add(1) > gw.cfg.RateLimit.MaxWSConns {
		gw.rl.wsConns.Add(-1)
		return false, nil
	}
	return true, func() {
		gw.rl.wsConns.Add(-1)
	}
}

