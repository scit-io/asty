package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// applyEnvOverrides lets an operator override individual fields with
// environment variables. Designed for dev/CI: bringing up a node with
// a single tweak (A_LOG_LEVEL=debug) is friendlier than editing YAML.
// Anything not set in the environment is left untouched.
func applyEnvOverrides(c *Config) {
	envStr("A_DOMAIN", &c.Domain)
	envStr("A_DATACENTER", &c.Datacenter)
	envStr("A_NODE_ID", &c.NodeID)
	envStr("A_NODE_IP", &c.NodeIP)
	envStr("A_TOKEN", &c.Token)
	envStr("A_LOG_LEVEL", &c.LogLevel)
	envBool("A_DEV_MODE", &c.DevMode)
	envInt("A_MOCK_NODES", &c.MockNodes)

	envStr("A_NATS_HOST", &c.NATS.Host)
	envStr("A_NATS_PORT", &c.NATS.Port)
	envStr("A_NATS_USER", &c.NATS.User)
	envStr("A_NATS_PASSWORD", &c.NATS.Password)

	envInt("A_MIN_COPIES", &c.Autoscale.MinCopies)
	envInt("A_TARGET_CPU", &c.Autoscale.TargetCPU)
	envInt("A_TARGET_MEMORY", &c.Autoscale.TargetMemory)
	envInt("A_TRAFFIC_RPS_THRESHOLD", &c.Autoscale.TrafficRPSThreshold)
	envDur("A_TRAFFIC_WINDOW", &c.Autoscale.TrafficWindow)
	envDur("A_COOLDOWN_UP", &c.Autoscale.CooldownUp)
	envDur("A_COOLDOWN_DOWN", &c.Autoscale.CooldownDown)
	envDur("A_EVAL_INTERVAL", &c.Autoscale.EvalInterval)
	envStr("A_DC_LATENCY", &c.Autoscale.DCLatency)
	envInt("A_CONTROLLER_WORKERS", &c.Autoscale.ControllerWorkers)

	envInt("A_RESERVED_CPU", &c.Resources.ReservedCPU)
	envInt("A_RESERVED_MEMORY", &c.Resources.ReservedMemory)

	envStr("A_HTTP_ADDR", &c.HTTP.Addr)
	envStr("A_WORK_DIR", &c.Agent.WorkDir)
	envStr("A_SERVICE_DIR", &c.Agent.ServiceDir)

	applyGatewayEnv(&c.Gateway)
}

func applyGatewayEnv(g *GatewayConfig) {
	envBool("A_GATEWAY_ENABLED", &g.Enabled)

	envStr("A_HTTP_ADDR", &g.HTTP.Addr)
	envDur("A_HTTP_READ_HEADER_TIMEOUT", &g.HTTP.ReadHeaderTimeout)
	envDur("A_HTTP_READ_TIMEOUT", &g.HTTP.ReadTimeout)
	envDur("A_HTTP_WRITE_TIMEOUT", &g.HTTP.WriteTimeout)
	envDur("A_HTTP_IDLE_TIMEOUT", &g.HTTP.IdleTimeout)
	envDur("A_GATEWAY_NATS_REQUEST_TIMEOUT", &g.HTTP.NATSRequestTimeout)
	envDur("A_GATEWAY_NATS_RETRY_DELAY", &g.HTTP.NATSRetryDelay)
	envDur("A_GATEWAY_WS_CONNECT_TIMEOUT", &g.HTTP.WSConnectTimeout)
	envStr("A_GATEWAY_METRICS_ADDR", &g.MetricsAddr)

	if raw, ok := os.LookupEnv("A_ALLOWED_HOSTS"); ok {
		g.AllowedHosts = splitCSV(raw)
	}

	envFloat("A_GATEWAY_RATE_LIMIT", &g.RateLimit.Rate)
	envInt("A_GATEWAY_RATE_BURST", &g.RateLimit.Burst)
	envInt64("A_GATEWAY_MAX_WS_CONNS", &g.RateLimit.MaxWSConns)
	envStr("A_GATEWAY_TRUSTED_PROXY", &g.RateLimit.TrustedProxy)
	envInt("A_GATEWAY_RATE_LIMIT_MAX_IPS", &g.RateLimit.MaxIPs)
}

// splitCSV splits "a,b , c" into ["a","b","c"], trimming each element
// and skipping blanks. Used for env vars that carry a list value.
func splitCSV(raw string) []string {
	out := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func envStr(key string, dst *string) {
	if v, ok := os.LookupEnv(key); ok {
		*dst = v
	}
}
func envBool(key string, dst *bool) {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			*dst = b
		}
	}
}
func envInt(key string, dst *int) {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			*dst = n
		}
	}
}
func envInt64(key string, dst *int64) {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			*dst = n
		}
	}
}
func envFloat(key string, dst *float64) {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			*dst = f
		}
	}
}
func envDur(key string, dst *time.Duration) {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			*dst = d
		}
	}
}
