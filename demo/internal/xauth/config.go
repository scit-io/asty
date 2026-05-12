package xauth

import (
	"net/http"
	"os"
	"strings"
	"time"
)

type Config struct {
	NATSUrl        string
	KVBucket       string
	Username       string
	Password       string
	AccessSecret   []byte
	RefreshSecret  []byte
	AccessTTL      time.Duration
	RefreshTTL     time.Duration
	CookieDomain   string
	CookieSecure   bool
	CookieSameSite http.SameSite
}

func parseSameSite(s string) http.SameSite {
	switch strings.ToLower(s) {
	case "none":
		return http.SameSiteNoneMode
	case "lax":
		return http.SameSiteLaxMode
	default:
		return http.SameSiteStrictMode
	}
}

func LoadConfig() Config {
	host := envOr("A_NATS_HOST", "127.0.0.1")
	port := envOr("A_NATS_PORT", "4222")
	natsURL := "nats://" + host + ":" + port

	user := os.Getenv("A_NATS_USER")
	pass := os.Getenv("A_NATS_PASSWORD")
	if user != "" && pass != "" {
		natsURL = "nats://" + user + ":" + pass + "@" + host + ":" + port
	}

	kvBucket := envOr("A_KV_AUTHMS_REFRESH_TOKENS", "authms_refresh_tokens")

	return Config{
		NATSUrl:        natsURL,
		KVBucket:       kvBucket,
		Username:       mustEnv("X_AUTH_USERNAME"),
		Password:       mustEnv("X_AUTH_PASSWORD"),
		AccessSecret:   []byte(mustEnv("X_AUTH_ACCESS_SECRET")),
		RefreshSecret:  []byte(mustEnv("X_AUTH_REFRESH_SECRET")),
		AccessTTL:      durationOr("X_AUTH_ACCESS_TTL", 15*time.Minute),
		RefreshTTL:     durationOr("X_AUTH_REFRESH_TTL", 168*time.Hour),
		CookieDomain:   os.Getenv("X_AUTH_COOKIE_DOMAIN"),
		CookieSecure:   envOr("X_AUTH_COOKIE_SECURE", "true") == "true",
		CookieSameSite: parseSameSite(envOr("X_AUTH_COOKIE_SAMESITE", "strict")),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic("required env var not set: " + key)
	}
	return v
}

func durationOr(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
