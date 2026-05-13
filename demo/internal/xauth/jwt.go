package xauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
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
