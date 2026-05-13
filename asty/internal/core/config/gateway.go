package config

import (
	"fmt"
	"time"
)

// GatewayConfig governs the embedded HTTP entry point that runs inside
// the asty agent process. When Enabled is false, the agent skips the
// gateway entirely — useful for control-plane-only nodes.
type GatewayConfig struct {
	Enabled      bool                   `yaml:"enabled"`
	HTTP         GatewayHTTPConfig      `yaml:"http"`
	MetricsAddr  string                 `yaml:"metrics_addr"`
	AllowedHosts []string               `yaml:"allowed_hosts"`
	RateLimit    GatewayRateLimitConfig `yaml:"rate_limit"`
}

// GatewayHTTPConfig — incoming HTTP-server parameters for the gateway.
type GatewayHTTPConfig struct {
	Addr               string        `yaml:"addr"`
	ReadHeaderTimeout  time.Duration `yaml:"read_header_timeout"`
	ReadTimeout        time.Duration `yaml:"read_timeout"`
	WriteTimeout       time.Duration `yaml:"write_timeout"`
	IdleTimeout        time.Duration `yaml:"idle_timeout"`
	NATSRequestTimeout time.Duration `yaml:"nats_request_timeout"`
	NATSRetryDelay     time.Duration `yaml:"nats_retry_delay"`
	WSConnectTimeout   time.Duration `yaml:"ws_connect_timeout"`
}

// GatewayRateLimitConfig — general per-IP cap and global WS counter.
// Path-specific rules (brute-force defense, write throttling) are declared
// by services in their .asty files and collected by the gateway at startup.
type GatewayRateLimitConfig struct {
	Rate         float64 `yaml:"rate"`
	Burst        int     `yaml:"burst"`
	MaxWSConns   int64   `yaml:"max_ws_conns"`
	TrustedProxy string  `yaml:"trusted_proxy"`
	MaxIPs       int     `yaml:"max_ips"`
}

// Validate rejects misconfiguration up front. Zero or negative values
// passed env-only loading silently and caused hard-to-spot regressions
// at runtime (rate.Limit(0) rejects every request; MaxIPs<=0 breaks
// eviction).
func (g GatewayConfig) Validate() error {
	if !g.Enabled {
		return nil
	}
	rl := g.RateLimit
	if rl.Rate <= 0 {
		return fmt.Errorf("gateway.rate_limit.rate must be > 0, got %v", rl.Rate)
	}
	if rl.Burst <= 0 {
		return fmt.Errorf("gateway.rate_limit.burst must be > 0, got %d", rl.Burst)
	}
	if rl.MaxIPs <= 0 {
		return fmt.Errorf("gateway.rate_limit.max_ips must be > 0, got %d", rl.MaxIPs)
	}
	if rl.MaxWSConns <= 0 {
		return fmt.Errorf("gateway.rate_limit.max_ws_conns must be > 0, got %d", rl.MaxWSConns)
	}
	return nil
}

func gatewayDefaults() GatewayConfig {
	return GatewayConfig{
		Enabled: true,
		HTTP: GatewayHTTPConfig{
			Addr:               ":80",
			ReadHeaderTimeout:  5 * time.Second,
			ReadTimeout:        15 * time.Second,
			WriteTimeout:       15 * time.Second,
			IdleTimeout:        60 * time.Second,
			NATSRequestTimeout: 5 * time.Second,
			NATSRetryDelay:     100 * time.Millisecond,
			WSConnectTimeout:   2 * time.Second,
		},
		MetricsAddr: "127.0.0.1:8081",
		RateLimit: GatewayRateLimitConfig{
			Rate:       100,
			Burst:      200,
			MaxWSConns: 1000,
			MaxIPs:     100_000,
		},
	}
}
