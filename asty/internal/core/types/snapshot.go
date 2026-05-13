package types

// ClusterSnapshot is the immutable per-tick state shared with all SSE subscribers.
type ClusterSnapshot struct {
	Timestamp int64

	Cluster ClusterStatusPayload
	Nodes   []*NodeInfo

	Services []ServiceWithUsage

	AllocsByNode    map[string][]*ServiceAllocation
	AllocsByService map[string][]*ServiceAllocation
	AllocByID       map[string]*ServiceAllocation
}

type ClusterStatusPayload struct {
	Leader       string `json:"leader"`
	LeaderIP     string `json:"leader_ip"`
	IsLeader     bool   `json:"is_leader"`
	NodesTotal   int    `json:"nodes_total"`
	NodesHealthy int    `json:"nodes_healthy"`
}

type ServiceWithUsage struct {
	*ServiceDefinition

	CurrentCopies      int     `json:"current_copies"`
	AvgCPUPercent      float64 `json:"avg_cpu_percent"`
	AvgMemoryPercent   float64 `json:"avg_memory_percent"`
	AvgCPUMHz          float64 `json:"avg_cpu_mhz"`
	AvgMemoryMB        float64 `json:"avg_memory_mb"`

	MinCopies          int    `json:"min_copies"`
	TargetCPU          int    `json:"target_cpu"`
	TargetMemory       int    `json:"target_memory"`
	TrafficThreshold   int    `json:"traffic_threshold"`
	CooldownUpActive   bool   `json:"cooldown_up_active"`
	CooldownDownActive bool   `json:"cooldown_down_active"`
	LastAction         string `json:"last_action,omitempty"`
	LastActionAt       int64  `json:"last_action_at,omitempty"`
}
