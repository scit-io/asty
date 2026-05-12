package xws

import (
	"os"
	"time"
)

type Config struct {
	NATSUrl           string
	InactivityTimeout time.Duration
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

	return Config{
		NATSUrl:           natsURL,
		InactivityTimeout: durationOr("X_WS_INACTIVITY_TIMEOUT", 3*time.Minute),
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
