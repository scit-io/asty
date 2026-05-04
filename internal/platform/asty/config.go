package asty

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for Asty agent and server
type Config struct {
	// Cluster
	Domain     string
	Datacenter string
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
}

// LoadConfig loads configuration from environment variables (A_* prefix)
func LoadConfig() (*Config, error) {
	cfg := &Config{
		// Cluster
		Domain:     getEnv("A_DOMAIN", ""),
		Datacenter: getEnv("A_DATACENTER", "dc1"),
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
		UIAddr: getEnv("A_UI_ADDR", "127.0.0.1:4646"),
	}

	// Validate required fields
	if cfg.Domain == "" {
		return nil, fmt.Errorf("A_DOMAIN is required")
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("A_TOKEN is required")
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
