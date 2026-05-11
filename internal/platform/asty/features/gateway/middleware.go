package gateway

import (
	"net"
	"net/http"
	"strings"

	"asty/internal/platform/metrics"
)

// middlewareRateLimit applies per-IP rate limiting to /v1/ routes.
//
// Check order:
//  1. If rate_limit.auth_path_prefix is set and the path matches,
//     the strict per-IP auth cap is checked first (brute-force defense).
//  2. Every /v1/ route is then checked against the general cap.
//
// /health bypasses this chain entirely.
func (gw *Gateway) middlewareRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := realIP(r, gw.cfg.RateLimit.TrustedProxy)

		if p := gw.cfg.RateLimit.AuthPathPrefix; p != "" && strings.HasPrefix(r.URL.Path, p) {
			if !gw.rl.allowAuth(ip) {
				gw.log.Warn().Str("ip", ip).Str("path", r.URL.Path).Msg("auth rate limit exceeded")
				metrics.RateLimitRejectedTotal.WithLabelValues("auth").Inc()
				writeRateLimited(w)
				return
			}
		}

		if !gw.rl.allow(ip) {
			gw.log.Warn().Str("ip", ip).Str("path", r.URL.Path).Msg("rate limit exceeded")
			metrics.RateLimitRejectedTotal.WithLabelValues("general").Inc()
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
	metrics.WSConnectionsActive.Inc()
	return true, func() {
		gw.rl.wsConns.Add(-1)
		metrics.WSConnectionsActive.Dec()
	}
}

// realIP returns the client IP. X-Real-IP is accepted only when the
// request came from trustedProxy (Cloudflare, LB). Empty trustedProxy
// or a mismatch falls back to r.RemoteAddr.
//
// X-Real-IP is parsed with net.ParseIP: an invalid value from the
// proxy would land in the LRU rate-limit table as a unique key —
// garbage-DoS on the table. Fallback is the same r.RemoteAddr as the
// no-header case.
func realIP(r *http.Request, trustedProxy string) string {
	if trustedProxy != "" {
		remoteIP, _, _ := net.SplitHostPort(r.RemoteAddr)
		if remoteIP == trustedProxy {
			if ip := r.Header.Get("X-Real-IP"); ip != "" {
				if parsed := net.ParseIP(ip); parsed != nil {
					return parsed.String()
				}
			}
		}
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
