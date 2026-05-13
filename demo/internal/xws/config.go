package xws

import (
	"time"

	"asty/demo/utils"
)

type Config struct {
	NATSUrl           string
	InactivityTimeout time.Duration
}

func LoadConfig() Config {
	return Config{
		NATSUrl:           utils.NATSURL(),
		InactivityTimeout: utils.DurationOr("X_WS_INACTIVITY_TIMEOUT", 3*time.Minute),
	}
}
