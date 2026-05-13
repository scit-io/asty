package envutil

import "os"

// EnvOr returns the value of env var key, or fallback if unset/empty.
func EnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// MustEnv returns the value of env var key, or panics if it's unset.
// Use for secrets and other config with no sensible default — failing
// loud at startup beats silent misbehaviour later.
func MustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic("required env var not set: " + key)
	}
	return v
}
