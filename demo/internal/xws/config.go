package xws

import (
	"time"

	"asty/demo/utils"
)

type Config struct {
	NATSUrl           string
	AccessSecret      []byte
	InactivityTimeout time.Duration
}

func LoadConfig() Config {
	return Config{
		NATSUrl:           utils.NATSURL(),
		AccessSecret:      []byte(utils.MustEnv("X_AUTH_ACCESS_SECRET")),
		InactivityTimeout: utils.DurationOr("X_WS_INACTIVITY_TIMEOUT", 3*time.Minute),
	}
}
