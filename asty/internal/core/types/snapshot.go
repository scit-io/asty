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
	// LeaderDC is the leader's datacenter (NodeInfo.Datacenter on the
	// leader). Surfaced so the cluster page can show the leader's
	// location without a second lookup against snap.Nodes.
	LeaderDC     string `json:"leader_dc,omitempty"`
	// LeaderHost is the leader's public DNS name (NodeInfo.Host on the
	// leader). Empty when the leader hasn't been assigned a host name.
	// Surfaced alongside LeaderIP so frontends that address nodes by
	// name (e.g. for sticky balancing) don't need a second lookup.
	LeaderHost   string `json:"leader_host,omitempty"`
	IsLeader     bool   `json:"is_leader"`
	NodesTotal   int    `json:"nodes_total"`
	NodesHealthy int    `json:"nodes_healthy"`
	// ServedBy is the id of the node that produced this snapshot — the
	// one the dashboard is currently talking to. Lets the UI show which
	// node answered even behind a load balancer, where the browser
	// otherwise can't tell which backend served the request.
	ServedBy string `json:"served_by"`
}

type ServiceWithUsage struct {
	*ServiceDefinition

	CurrentCopies    int     `json:"current_copies"`
	AvgCPUPercent    float64 `json:"avg_cpu_percent"`
	AvgMemoryPercent float64 `json:"avg_memory_percent"`
	AvgCPUMHz        float64 `json:"avg_cpu_mhz"`
	AvgMemoryMB      float64 `json:"avg_memory_mb"`

	MinCopies          int           `json:"min_copies"`
	TargetCPU          int           `json:"target_cpu"`
	TargetMemory       int           `json:"target_memory"`
	TrafficThreshold   int           `json:"traffic_threshold"`
	CooldownUpActive   bool          `json:"cooldown_up_active"`
	CooldownDownActive bool          `json:"cooldown_down_active"`
	LastAction         ScalingAction `json:"last_action,omitempty"`
	LastActionAt       int64         `json:"last_action_at,omitempty"`
	// LastReason mirrors the most recent scaling-event reason so the
	// dashboard can distinguish autoscaler decisions from manual
	// operator actions (reason starts with "manual:") in the list view.
	LastReason string `json:"last_reason,omitempty"`
	// Latest deployment record for the service — exposed so the UI's
	// "Last action" column can pick the more recent of {scaling event,
	// deploy} without a parallel fetch per row.
	LastDeployVersion string `json:"last_deploy_version,omitempty"`
	LastDeployStatus  string `json:"last_deploy_status,omitempty"`
	LastDeployAt      int64  `json:"last_deploy_at,omitempty"`
}
