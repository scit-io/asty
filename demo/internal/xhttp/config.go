package xhttp

import (
	"os"
	"time"
)

type Config struct {
	NATSUrl      string
	KVBucket     string
	DatabaseURL  string
	AccessSecret []byte
	CacheTTL     time.Duration
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

	dbURL := os.Getenv("X_HTTP_DATABASE_URL")
	if dbURL == "" {
		panic("required env var not set: X_HTTP_DATABASE_URL")
	}
	accessSecret := os.Getenv("X_AUTH_ACCESS_SECRET")
	if accessSecret == "" {
		panic("required env var not set: X_AUTH_ACCESS_SECRET")
	}

	kvBucket := envOr("A_KV_XHTTP_CACHE", "xhttp_cache")

	return Config{
		NATSUrl:      natsURL,
		KVBucket:     kvBucket,
		DatabaseURL:  dbURL,
		AccessSecret: []byte(accessSecret),
		CacheTTL:     durationOr("X_HTTP_CACHE_TTL", 30*time.Second),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
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
