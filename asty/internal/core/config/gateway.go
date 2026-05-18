package config

import (
	"fmt"
	"time"
)

// GatewayConfig governs the embedded HTTP entry point that runs inside
// the asty agent process. When Enabled is false, the agent skips the
// gateway entirely — useful for control-plane-only nodes.
//
// Host/Port/Prefix replace the older HTTP.Addr field: Host+Port build
// the bind address, Prefix is the path namespace user traffic enters
// through (default /api/v1). The path-validation regex inside the
// router only cares about segments AFTER the prefix, so changing it
// is a one-line config update.
type GatewayConfig struct {
	Enabled      bool                   `yaml:"enabled"`
	Host         string                 `yaml:"host"`   // bind host (default 0.0.0.0)
	Port         int                    `yaml:"port"`   // default 80
	Prefix       string                 `yaml:"prefix"` // default /api/v1
	HTTP         GatewayHTTPConfig      `yaml:"http"`
	AllowedHosts []string               `yaml:"allowed_hosts"`
	RateLimit    GatewayRateLimitConfig `yaml:"rate_limit"`
}

// Addr returns "<host>:<port>" — http.Server.Addr form. Empty host
// becomes 0.0.0.0 because the gateway is user-facing by design,
// unlike the dashboard and Prometheus endpoints which default to
// 127.0.0.1.
func (g GatewayConfig) Addr() string {
	host := g.Host
	if host == "" {
		host = "0.0.0.0"
	}
	return fmt.Sprintf("%s:%d", host, g.Port)
}

// GatewayHTTPConfig — incoming HTTP-server parameters for the gateway.
// Addr stays here for now as a fallback for old configs that still
// set gateway.http.addr; new deployments should use Host/Port on
// GatewayConfig instead.
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
		Host:    "0.0.0.0",
		Port:    80,
		Prefix:  "/api/v1",
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
		RateLimit: GatewayRateLimitConfig{
			Rate:       100,
			Burst:      200,
			MaxWSConns: 1000,
			MaxIPs:     100_000,
		},
	}
}
