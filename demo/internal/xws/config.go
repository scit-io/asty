package xws

import (
	"time"

	"asty/demo/internal/envutil"
)

type Config struct {
	NATSUrl           string
	InactivityTimeout time.Duration
}

func LoadConfig() Config {
	return Config{
		NATSUrl:           envutil.NATSURL(),
		InactivityTimeout: envutil.DurationOr("X_WS_INACTIVITY_TIMEOUT", 3*time.Minute),
	}
}
