package xauth

import (
	"net/http"
	"os"
	"strings"
	"time"

	"asty/demo/utils"
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
	return Config{
		NATSUrl:        utils.NATSURL(),
		KVBucket:       utils.EnvOr("A_KV_AUTHMS_REFRESH_TOKENS", "authms_refresh_tokens"),
		Username:       utils.MustEnv("X_AUTH_USERNAME"),
		Password:       utils.MustEnv("X_AUTH_PASSWORD"),
		AccessSecret:   []byte(utils.MustEnv("X_AUTH_ACCESS_SECRET")),
		RefreshSecret:  []byte(utils.MustEnv("X_AUTH_REFRESH_SECRET")),
		AccessTTL:      utils.DurationOr("X_AUTH_ACCESS_TTL", 15*time.Minute),
		RefreshTTL:     utils.DurationOr("X_AUTH_REFRESH_TTL", 168*time.Hour),
		CookieDomain:   os.Getenv("X_AUTH_COOKIE_DOMAIN"),
		CookieSecure:   utils.EnvOr("X_AUTH_COOKIE_SECURE", "true") == "true",
		CookieSameSite: parseSameSite(utils.EnvOr("X_AUTH_COOKIE_SAMESITE", "strict")),
	}
}
