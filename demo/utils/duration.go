// Package utils holds tiny helpers shared by the demo services
// (env-var parsing, NATS URL building, micro reply formatting). Kept
// separate from asty/internal/core/* on purpose: demos are meant to
// look like a third-party service would — no dependency on the asty
// SDK, just stdlib and nats.go.
package utils

import (
	"os"
	"time"
)

// DurationOr reads key from the environment and parses it as a
// duration. Falls back to the provided default for an empty value or a
// parse error. Note: literal "0s" is honoured (caller may use it to
// disable a timeout/TTL); compare with asty's ParseDurationOr which
// treats "0s" as misconfiguration.
func DurationOr(key string, fallback time.Duration) time.Duration {
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
