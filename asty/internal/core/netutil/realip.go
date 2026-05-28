package netutil

import (
	"net"
	"net/http"
)

// RealIP returns the client IP for an inbound HTTP request. X-Real-IP
// is accepted only when r.RemoteAddr matches trustedProxy (e.g. the LB
// or CDN in front of us); empty trustedProxy or a mismatch falls back
// to r.RemoteAddr. We deliberately do NOT honour XFF on every request
// because that's the standard header-spoof vector — only when an
// operator has explicitly named a trusted proxy.
func RealIP(r *http.Request, trustedProxy string) string {
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
