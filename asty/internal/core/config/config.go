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
// service definitions in ops/deployer/loader.go.
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
	HTTP      HTTPConfig      `yaml:"http"`
	Agent     AgentConfig     `yaml:"agent"`
	Gateway   GatewayConfig   `yaml:"gateway"`
	Artifact  ArtifactConfig  `yaml:"artifact"`
}

// AutoscaleConfig — controller and scaling thresholds. MaxCopies and
// IdleHold come from TZ §5.2: the former caps runaway scale-up, the
// latter requires the service to stay idle for a continuous window
// before a scale-down decision fires (anti-flap hysteresis on top of
// the cooldown gate).
type AutoscaleConfig struct {
	MinCopies           int           `yaml:"min_copies"`
	MaxCopies           int           `yaml:"max_copies"`
	TargetCPU           int           `yaml:"target_cpu"`
	TargetMemory        int           `yaml:"target_memory"`
	TrafficRPSThreshold int           `yaml:"traffic_rps_threshold"`
	TrafficWindow       time.Duration `yaml:"traffic_window"`
	CooldownUp          time.Duration `yaml:"cooldown_up"`
	CooldownDown        time.Duration `yaml:"cooldown_down"`
	IdleHold            time.Duration `yaml:"idle_hold"`
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

// HTTPConfig — where the orchestrator's HTTP surface listens on each
// node. Serves SSE streams, polling endpoints (incl. Prometheus
// /metrics via content-negotiation), and command POSTs.
type HTTPConfig struct {
	Addr string `yaml:"addr"`
}

// AgentConfig — agent-specific paths and capacity overrides. Capacity
// overrides used to be read with os.Getenv at the point of use; they
// now live here as part of the single-config-path discipline (TZ §2.9).
type AgentConfig struct {
	WorkDir    string             `yaml:"work_dir"`
	ServiceDir string             `yaml:"service_dir"`
	Capacity   AgentCapacityConfig `yaml:"capacity"`
}

// AgentCapacityConfig overrides the values detect*() helpers would
// produce from the host. Used in dev to fake a heterogeneous cluster
// from a single physical machine. Values that mean "no override":
//   - CPUTotal, MemoryTotal, DiskTotal, SwapTotal: 0
//   - DiskOSBaseline, NATSDiskBaseline: negative (sentinel for "use
//     defaults"); zero is a legitimate explicit value.
//   - DiskType: empty string.
type AgentCapacityConfig struct {
	CPUTotal         int    `yaml:"cpu_total"`           // MHz aggregate
	MemoryTotal      int64  `yaml:"memory_total"`        // MB
	DiskTotal        int64  `yaml:"disk_total"`          // MB
	SwapTotal        int64  `yaml:"swap_total"`          // MB
	DiskOSBaseline   int64  `yaml:"disk_os_baseline"`    // MB (-1 unset)
	NATSDiskBaseline int64  `yaml:"nats_disk_baseline"`  // MB (-1 unset)
	DiskType         string `yaml:"disk_type"`           // ssd|hdd
}

// ArtifactConfig holds template variables substituted into artifact
// URLs (server-side). Lives at top-level because both server and dev
// tooling refer to it; the .asty's per-service "artifact.url" expands
// these via os.Expand at deploy time.
type ArtifactConfig struct {
	Arch       string `yaml:"arch"`
	GitHubRepo string `yaml:"github_repo"`
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
	if err := c.NATS.Validate(); err != nil {
		return err
	}
	return c.Gateway.Validate()
}

// defaults returns a Config populated with the sane defaults the agent
// and server fall back to when a field is absent from both YAML and env.
func defaults() *Config {
	return &Config{
		Datacenter: "dc1",
		LogLevel:   "info",
		NATS:       natsDefaults(),
		Autoscale: AutoscaleConfig{
			MinCopies:           3,
			MaxCopies:           0, // 0 = unlimited (cluster-size cap is the only ceiling)
			TargetCPU:           75,
			TargetMemory:        75,
			TrafficRPSThreshold: 5,
			TrafficWindow:       time.Minute,
			CooldownUp:          30 * time.Second,
			CooldownDown:        5 * time.Minute,
			IdleHold:            5 * time.Minute,
			EvalInterval:        10 * time.Second,
			ControllerWorkers:   2,
		},
		Resources: ResourcesConfig{
			ReservedCPU:    100,
			ReservedMemory: 250,
		},
		HTTP: HTTPConfig{Addr: "127.0.0.1:8080"},
		Agent: AgentConfig{
			WorkDir:    "/var/lib/asty",
			ServiceDir: "/etc/asty/services",
			Capacity: AgentCapacityConfig{
				DiskOSBaseline:   -1, // sentinel "use ratio default"
				NATSDiskBaseline: -1, // sentinel "use NATS default"
			},
		},
		Gateway:  gatewayDefaults(),
		Artifact: ArtifactConfig{},
	}
}
