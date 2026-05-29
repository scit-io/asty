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

	envStr("A_NATS_USER", &c.NATS.User)
	envStr("A_NATS_PASSWORD", &c.NATS.Password)
	envStr("A_NATS_OBSERVER_USER", &c.NATS.ObserverUser)
	envStr("A_NATS_OBSERVER_PASSWORD", &c.NATS.ObserverPassword)
	envStr("A_NATS_APP_USER", &c.NATS.AppUser)
	envStr("A_NATS_APP_PASSWORD", &c.NATS.AppPassword)
	envStr("A_NATS_PEERS_FILE", &c.NATS.PeersFile)
	envStr("A_NATS_PEERS", &c.NATS.Peers)

	envInt("A_MIN_COPIES", &c.Autoscale.MinCopies)
	envInt("A_MAX_COPIES", &c.Autoscale.MaxCopies)
	envInt("A_TARGET_CPU", &c.Autoscale.TargetCPU)
	envInt("A_TARGET_MEMORY", &c.Autoscale.TargetMemory)
	envInt("A_TRAFFIC_RPS_THRESHOLD", &c.Autoscale.TrafficRPSThreshold)
	envDur("A_TRAFFIC_WINDOW", &c.Autoscale.TrafficWindow)
	envDur("A_COOLDOWN_UP", &c.Autoscale.CooldownUp)
	envDur("A_COOLDOWN_DOWN", &c.Autoscale.CooldownDown)
	envDur("A_IDLE_HOLD", &c.Autoscale.IdleHold)
	envDur("A_EVAL_INTERVAL", &c.Autoscale.EvalInterval)
	envStr("A_DC_LATENCY", &c.Autoscale.DCLatency)
	envInt("A_CONTROLLER_WORKERS", &c.Autoscale.ControllerWorkers)

	envInt("A_RESERVED_CPU", &c.Resources.ReservedCPU)
	envInt("A_RESERVED_MEMORY", &c.Resources.ReservedMemory)

	envStr("A_DASHBOARD_HOST", &c.Dashboard.Host)
	envInt("A_DASHBOARD_PORT", &c.Dashboard.Port)
	envStr("A_DASHBOARD_PREFIX", &c.Dashboard.Prefix)
	if raw, ok := os.LookupEnv("A_DASHBOARD_ALLOWED_ORIGINS"); ok {
		c.Dashboard.AllowedOrigins = splitCSV(raw)
	}

	envStr("A_PROMETHEUS_HOST", &c.Prometheus.Host)
	envInt("A_PROMETHEUS_PORT", &c.Prometheus.Port)
	envStr("A_PROMETHEUS_PREFIX", &c.Prometheus.Prefix)

	envStr("A_WORK_DIR", &c.Agent.WorkDir)
	envStr("A_SERVICE_DIR", &c.Agent.ServiceDir)

	envInt("A_CPU_TOTAL", &c.Agent.Capacity.CPUTotal)
	envInt64("A_MEMORY_TOTAL", &c.Agent.Capacity.MemoryTotal)
	envInt64("A_DISK_TOTAL", &c.Agent.Capacity.DiskTotal)
	envInt64("A_SWAP_TOTAL", &c.Agent.Capacity.SwapTotal)
	envInt64("A_DISK_OS_BASELINE", &c.Agent.Capacity.DiskOSBaseline)
	envInt64("A_NATS_DISK_BASELINE", &c.Agent.Capacity.NATSDiskBaseline)
	envStr("A_DISK_TYPE", &c.Agent.Capacity.DiskType)

	envStr("A_ARCH", &c.Artifact.Arch)
	envStr("A_GITHUB_REPO", &c.Artifact.GitHubRepo)

	applyGatewayEnv(&c.Gateway)
}

func applyGatewayEnv(g *GatewayConfig) {
	envBool("A_GATEWAY_ENABLED", &g.Enabled)

	envStr("A_GATEWAY_HOST", &g.Host)
	envInt("A_GATEWAY_PORT", &g.Port)
	envStr("A_GATEWAY_PREFIX", &g.Prefix)

	envDur("A_GATEWAY_READ_HEADER_TIMEOUT", &g.HTTP.ReadHeaderTimeout)
	envDur("A_GATEWAY_READ_TIMEOUT", &g.HTTP.ReadTimeout)
	envDur("A_GATEWAY_WRITE_TIMEOUT", &g.HTTP.WriteTimeout)
	envDur("A_GATEWAY_IDLE_TIMEOUT", &g.HTTP.IdleTimeout)
	envDur("A_GATEWAY_NATS_REQUEST_TIMEOUT", &g.HTTP.NATSRequestTimeout)
	envDur("A_GATEWAY_NATS_RETRY_DELAY", &g.HTTP.NATSRetryDelay)
	envDur("A_GATEWAY_WS_CONNECT_TIMEOUT", &g.HTTP.WSConnectTimeout)

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
