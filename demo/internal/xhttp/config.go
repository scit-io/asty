package xhttp

import (
	"time"

	"asty/demo/utils"
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
		NATSUrl:      utils.NATSURL(),
		KVBucket:     utils.EnvOr("A_KV_XHTTP_CACHE", "xhttp_cache"),
		DatabaseURL:  utils.MustEnv("X_HTTP_DATABASE_URL"),
		AccessSecret: []byte(utils.MustEnv("X_AUTH_ACCESS_SECRET")),
		CacheTTL:     utils.DurationOr("X_HTTP_CACHE_TTL", 30*time.Second),
	}
}
