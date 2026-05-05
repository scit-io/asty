package asty

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for Asty agent and server
type Config struct {
	// Development mode
	DevMode bool `env:"A_DEV_MODE" envDefault:"false"`
	MockNodes int `env:"A_MOCK_NODES" envDefault:"0"` // Number of mock nodes to create in dev mode

	// Node identity
	// Cluster
	Domain     string
	Datacenter string
	NodeID     string
	NodeIP     string // Explicit node IP address (optional, auto-detected if not set)
	Token      string
	LogLevel   string

	// NATS transport
	NATSHost     string
	NATSPort     string
	NATSUser     string
	NATSPassword string

	// Autoscaling
	MinCopies            int
	TargetCPU            int
	TargetMemory         int
	TrafficRPSThreshold  int
	TrafficWindow        time.Duration
	CooldownUp           time.Duration
	CooldownDown         time.Duration
	EvalInterval         time.Duration
	DCLatency            string

	// Node resources
	ReservedCPU    int
	ReservedMemory int

	// UI
	UIAddr string

	// Agent work directory
	WorkDir string

	// Service definitions directory
	ServiceDir string
}

// LoadConfig loads configuration from environment variables (A_* prefix)
func LoadConfig() (*Config, error) {
	cfg := &Config{
		// Development
		DevMode:   getEnvBool("A_DEV_MODE", false),
		MockNodes: getEnvInt("A_MOCK_NODES", 0),

		// Cluster
		Domain:     getEnv("A_DOMAIN", ""),
		Datacenter: getEnv("A_DATACENTER", "dc1"),
		NodeID:     getEnv("A_NODE_ID", ""),
		NodeIP:     getEnv("A_NODE_IP", ""),
		Token:      getEnv("A_TOKEN", ""),
		LogLevel:   getEnv("A_LOG_LEVEL", "info"),

		// NATS
		NATSHost:     getEnv("A_NATS_HOST", "127.0.0.1"),
		NATSPort:     getEnv("A_NATS_PORT", "4222"),
		NATSUser:     getEnv("A_NATS_USER", ""),
		NATSPassword: getEnv("A_NATS_PASSWORD", ""),

		// Autoscaling defaults
		MinCopies:           getEnvInt("A_MIN_COPIES", 3),
		TargetCPU:           getEnvInt("A_TARGET_CPU", 75),
		TargetMemory:        getEnvInt("A_TARGET_MEMORY", 75),
		TrafficRPSThreshold: getEnvInt("A_TRAFFIC_RPS_THRESHOLD", 5),
		TrafficWindow:       getEnvDuration("A_TRAFFIC_WINDOW", time.Minute),
		CooldownUp:          getEnvDuration("A_COOLDOWN_UP", 30*time.Second),
		CooldownDown:        getEnvDuration("A_COOLDOWN_DOWN", 5*time.Minute),
		EvalInterval:        getEnvDuration("A_EVAL_INTERVAL", 10*time.Second),
		DCLatency:           getEnv("A_DC_LATENCY", ""),

		// Node resources
		ReservedCPU:    getEnvInt("A_RESERVED_CPU", 100),
		ReservedMemory: getEnvInt("A_RESERVED_MEMORY", 250),

		// UI
		UIAddr: getEnv("A_UI_ADDR", "127.0.0.1:4747"),

		// Agent
		WorkDir: getEnv("A_WORK_DIR", "/var/lib/asty"),

		// Service definitions
		ServiceDir: getEnv("A_SERVICE_DIR", "./deployments/infra"),
	}

	// Validate required fields (unless dev mode)
	if !cfg.DevMode {
		if cfg.Domain == "" {
			return nil, fmt.Errorf("A_DOMAIN is required")
		}
		if cfg.Token == "" {
			return nil, fmt.Errorf("A_TOKEN is required")
		}
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

