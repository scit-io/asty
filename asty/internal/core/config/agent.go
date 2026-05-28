package config

// AgentConfig — agent-specific paths and capacity overrides. Capacity
// overrides used to be read with os.Getenv at the point of use; they
// now live here as part of the single-config-path discipline.
//
// The drop-root target is NOT configurable — when the agent starts as
// root it drops to the dedicated `asty` user after bootstrap. See
// dropPrivileges in asty/internal/agent for the rationale: a single
// hardcoded name, no env wiring, no operator confusion about which
// name to use across machines.
type AgentConfig struct {
	WorkDir    string              `yaml:"work_dir"`
	ServiceDir string              `yaml:"service_dir"`
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
	CPUTotal         int    `yaml:"cpu_total"`          // MHz aggregate
	MemoryTotal      int64  `yaml:"memory_total"`       // MB
	DiskTotal        int64  `yaml:"disk_total"`         // MB
	SwapTotal        int64  `yaml:"swap_total"`         // MB
	DiskOSBaseline   int64  `yaml:"disk_os_baseline"`   // MB (-1 unset)
	NATSDiskBaseline int64  `yaml:"nats_disk_baseline"` // MB (-1 unset)
	DiskType         string `yaml:"disk_type"`          // ssd|hdd
}

// ArtifactConfig holds template variables substituted into artifact
// URLs (server-side). Lives at top-level because both server and dev
// tooling refer to it; the .asty's per-service "artifact.url" expands
// these via os.Expand at deploy time.
type ArtifactConfig struct {
	Arch       string `yaml:"arch"`
	GitHubRepo string `yaml:"github_repo"`
}
