package config

import (
	"fmt"
	"time"
)

// Config mirrors the on-disk config.asty schema. Sections (NATS,
// Autoscale, etc.) live in dedicated sub-structs so the YAML reads
// well and the Go code refers to grouped fields the same way.
//
// Loaded by Load() with the same yaml.Unmarshal pattern used for the
// service definitions in features/deployment/loader.go.
type Config struct {
	// Cluster identity
	Domain     string `yaml:"domain"`
	Datacenter string `yaml:"datacenter"`
	NodeID     string `yaml:"node_id"`
	NodeIP     string `yaml:"node_ip"`
	Token      string `yaml:"token"`
	LogLevel   string `yaml:"log_level"`

	// Development knobs
	DevMode   bool `yaml:"dev_mode"`
	MockNodes int  `yaml:"mock_nodes"`

	NATS      NATSConfig      `yaml:"nats"`
	Autoscale AutoscaleConfig `yaml:"autoscale"`
	Resources ResourcesConfig `yaml:"resources"`
	UI        UIConfig        `yaml:"ui"`
	Agent     AgentConfig     `yaml:"agent"`
	Gateway   GatewayConfig   `yaml:"gateway"`
}

// NATSConfig — where the local NATS sits and how to authenticate.
type NATSConfig struct {
	Host     string `yaml:"host"`
	Port     string `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

// AutoscaleConfig — controller and scaling thresholds.
type AutoscaleConfig struct {
	MinCopies           int           `yaml:"min_copies"`
	TargetCPU           int           `yaml:"target_cpu"`
	TargetMemory        int           `yaml:"target_memory"`
	TrafficRPSThreshold int           `yaml:"traffic_rps_threshold"`
	TrafficWindow       time.Duration `yaml:"traffic_window"`
	CooldownUp          time.Duration `yaml:"cooldown_up"`
	CooldownDown        time.Duration `yaml:"cooldown_down"`
	EvalInterval        time.Duration `yaml:"eval_interval"`
	DCLatency           string        `yaml:"dc_latency"`
	ControllerWorkers   int           `yaml:"controller_workers"`
}

// ResourcesConfig — what the agent reserves for itself before
// offering capacity to scheduled workloads.
type ResourcesConfig struct {
	ReservedCPU    int `yaml:"reserved_cpu"`
	ReservedMemory int `yaml:"reserved_memory"`
}

// UIConfig — where the read-only HTTP API listens on each node.
type UIConfig struct {
	Addr string `yaml:"addr"`
}

// AgentConfig — agent-specific paths.
type AgentConfig struct {
	WorkDir    string `yaml:"work_dir"`
	ServiceDir string `yaml:"service_dir"`
}

// Validate rejects required-field gaps early. Dev-mode opts out so a
// developer can spin a single-node cluster from defaults.
func (c *Config) Validate() error {
	if c.DevMode {
		return nil
	}
	if c.Domain == "" {
		return fmt.Errorf("domain is required (config.asty: domain, env: A_DOMAIN)")
	}
	if c.Token == "" {
		return fmt.Errorf("token is required (config.asty: token, env: A_TOKEN)")
	}
	return c.Gateway.Validate()
}

// defaults returns a Config populated with the sane defaults the agent
// and server fall back to when a field is absent from both YAML and env.
func defaults() *Config {
	return &Config{
		Datacenter: "dc1",
		LogLevel:   "info",
		NATS: NATSConfig{
			Host: "127.0.0.1",
			Port: "4222",
		},
		Autoscale: AutoscaleConfig{
			MinCopies:           3,
			TargetCPU:           75,
			TargetMemory:        75,
			TrafficRPSThreshold: 5,
			TrafficWindow:       time.Minute,
			CooldownUp:          30 * time.Second,
			CooldownDown:        5 * time.Minute,
			EvalInterval:        10 * time.Second,
			ControllerWorkers:   2,
		},
		Resources: ResourcesConfig{
			ReservedCPU:    100,
			ReservedMemory: 250,
		},
		UI: UIConfig{Addr: "127.0.0.1:4747"},
		Agent: AgentConfig{
			WorkDir:    "/var/lib/asty",
			ServiceDir: "./deployments/infra",
		},
		Gateway: gatewayDefaults(),
	}
}
