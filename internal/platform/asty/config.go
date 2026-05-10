package asty

import (
	"asty/internal/platform/asty/core/config"
)

// Type alias for backward compatibility
type Config = config.Config

// LoadConfig loads configuration from environment variables (A_* prefix)
func LoadConfig() (*Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

