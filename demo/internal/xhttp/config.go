package xhttp

import (
	"time"

	"asty/demo/internal/envutil"
)

type Config struct {
	NATSUrl      string
	KVBucket     string
	DatabaseURL  string
	AccessSecret []byte
	CacheTTL     time.Duration
}

func LoadConfig() Config {
	return Config{
		NATSUrl:      envutil.NATSURL(),
		KVBucket:     envutil.EnvOr("A_KV_XHTTP_CACHE", "xhttp_cache"),
		DatabaseURL:  envutil.MustEnv("X_HTTP_DATABASE_URL"),
		AccessSecret: []byte(envutil.MustEnv("X_AUTH_ACCESS_SECRET")),
		CacheTTL:     envutil.DurationOr("X_HTTP_CACHE_TTL", 30*time.Second),
	}
}
