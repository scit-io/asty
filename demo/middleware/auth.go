// Package middleware is the shared auth library for demo services.
//
// Asti (the orchestrator) is service-agnostic and does NOT import
// this package — demo services (xauth, xhttp, xws, ...) do. One
// JWT format, one verifier, one wrapper for protected endpoints:
//
//   - SignJWT / VerifyJWT / Claims / NewJTI — HS256 primitives,
//     used by xauth to issue tokens and by every other service
//     (via VerifyCookieToken / RequireAuthMicro) to check them.
//   - VerifyCookieToken — the full cookie → JWT → expiry pipeline.
//   - RequireAuthMicro — gates a nats.go/micro endpoint behind a
//     valid access_token cookie. On failure it calls req.Error(...),
//     which per ADR-32 sets Nats-Service-Error-Code +
//     Nats-Service-Error headers; the gateway reads those and maps
//     them to an HTTP status or WS close code.
//
// https://github.com/nats-io/nats-architecture-and-design/blob/main/adr/ADR-32.md
package middleware

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go/micro"
)

// jwtHeader is the immutable base64url-encoded header shared by every
// token (HS256). Computed once: base64url({"alg":"HS256","typ":"JWT"}).
const jwtHeader = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"

// JWTClockSkew is the allowed clock drift between the token signer and
// the verifier. Without it, a token issued "just now" on node A may
// appear "expired" on node B after a couple of seconds of NTP drift.
// 60 seconds is the industry standard (RFC 7519 §4.1.4 "small leeway");
// applied uniformly to access and refresh — to every JWT in the platform.
const JWTClockSkew = 60 * time.Second

// Claims is the JWT token payload (a subset of RFC 7519).
type Claims struct {
	Sub string `json:"sub"` // subject (username)
	Exp int64  `json:"exp"` // unix expiration time
	Jti string `json:"jti"` // unique token ID (used for refresh-token revocation)
	Iat int64  `json:"iat"` // unix issued-at time
}

// SignJWT produces a signed JWT (HS256) with no external dependencies.
// Format: base64url(header).base64url(payload).base64url(HMAC-SHA256(header.payload))
func SignJWT(c Claims, secret []byte) (string, error) {
	payload, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("jwt: marshal claims: %w", err)
	}
	hp := jwtHeader + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(hp))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return hp + "." + sig, nil
}

// VerifyJWT checks the HMAC-SHA256 signature of a token and returns
// its Claims.
//
// Expiry (Exp) is intentionally not checked here — the caller does that.
// Splitting these checks lets the client surface different messages:
// "token expired" (→ try /refresh) vs "token invalid" (→ log in again).
func VerifyJWT(token string, secret []byte) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, fmt.Errorf("jwt: invalid token format")
	}
	// Defense-in-depth: the token must have been signed by our SignJWT
	// (one fixed HS256 header). The HMAC check already rejects forgeries
	// without the secret, but an explicit header check fails fast and
	// guards against a future change that might start dispatching by
	// the alg field of the header.
	if parts[0] != jwtHeader {
		return Claims{}, fmt.Errorf("jwt: unsupported header")
	}
	hp := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(hp))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	// hmac.Equal runs in constant time — protection against timing attacks.
	if !hmac.Equal([]byte(parts[2]), []byte(expected)) {
		return Claims{}, fmt.Errorf("jwt: invalid signature")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, fmt.Errorf("jwt: decode payload: %w", err)
	}
	var c Claims
	if err := json.Unmarshal(payloadBytes, &c); err != nil {
		return Claims{}, fmt.Errorf("jwt: unmarshal claims: %w", err)
	}
	return c, nil
}

// NewJTI generates a cryptographically random unique token ID (JWT ID).
// Used to identify the refresh token in the KV store for revocation.
func NewJTI() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("jti: rand.Read: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// CookieValue returns the value of the named cookie from a raw HTTP
// Cookie header, or "" if absent. Uses net/http's parser so RFC 6265
// quoting / separators are handled correctly.
func CookieValue(header, name string) string {
	if header == "" {
		return ""
	}
	r := &http.Request{Header: http.Header{"Cookie": []string{header}}}
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

// VerifyCookieToken runs the cookie → JWT → expiry pipeline.
// Returns (claims, 0, "") on success; (zero, 401, msg) on any
// failure (cookie missing, JWT invalid, expired). The cookie name
// appears in the error message verbatim.
func VerifyCookieToken(cookieHeader, cookieName string, secret []byte) (Claims, int, string) {
	raw := CookieValue(cookieHeader, cookieName)
	if raw == "" {
		return Claims{}, 401, cookieName + " missing"
	}
	c, err := VerifyJWT(raw, secret)
	if err != nil {
		return Claims{}, 401, "invalid " + cookieName
	}
	if time.Now().Unix() > c.Exp+int64(JWTClockSkew.Seconds()) {
		return Claims{}, 401, cookieName + " expired"
	}
	return c, 0, ""
}

// RequireAuthMicro gates next behind a valid access_token cookie.
// Verification is local (no network call). On failure the wrapper
// emits an ADR-32 service-error; the gateway maps it to HTTP 401
// (or WS CloseCode 1008).
func RequireAuthMicro(secret []byte, next micro.HandlerFunc) micro.HandlerFunc {
	return func(req micro.Request) {
		_, code, errMsg := VerifyCookieToken(req.Headers().Get("Cookie"), "access_token", secret)
		if code != 0 {
			_ = req.Error(strconv.Itoa(code), errMsg, nil)
			return
		}
		next(req)
	}
}
