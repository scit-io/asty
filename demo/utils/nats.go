package utils

import "os"

// NATSURL assembles a NATS connection URL from the standard A_NATS_*
// environment variables. Returns "nats://user:pass@host:port" when both
// A_NATS_USER and A_NATS_PASSWORD are set, otherwise "nats://host:port".
// Defaults: 127.0.0.1:4222 — matches the per-node local NATS every
// demo service connects to.
func NATSURL() string {
	host := EnvOr("A_NATS_HOST", "127.0.0.1")
	port := EnvOr("A_NATS_PORT", "4222")
	user := os.Getenv("A_NATS_USER")
	pass := os.Getenv("A_NATS_PASSWORD")
	if user != "" && pass != "" {
		return "nats://" + user + ":" + pass + "@" + host + ":" + port
	}
	return "nats://" + host + ":" + port
}
