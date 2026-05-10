package types

import "time"

// nodeHeartbeatStaleAfter is the heartbeat-age threshold beyond which a
// node is considered unhealthy even if it last reported "ready". The agent
// publishes a heartbeat every 5 s, so 2 min is a wide safety margin that
// tolerates GC pauses and brief NATS reconnects without removing the node.
const nodeHeartbeatStaleAfter = 2 * time.Minute

// NodeInfo represents information about a cluster node
type NodeInfo struct {
	ID         string    `json:"id"`
	Datacenter string    `json:"datacenter"`
	IP         string    `json:"ip"`
	Status     string    `json:"status"` // ready, draining, down
	CreatedAt  time.Time `json:"created_at"`
	LastSeen   time.Time `json:"last_seen"`

	// Resources
	CPUTotal        int   `json:"cpu_total"`        // MHz
	CPUAvailable    int   `json:"cpu_available"`
	MemoryTotal     int64 `json:"memory_total"`     // MB
	MemoryAvailable int64 `json:"memory_available"`

	// Processes
	Processes []string `json:"processes"` // list of service names

	// Allocations counters (computed, not persisted in KV)
	AllocationsRunning int `json:"allocations_running"`
	AllocationsPlanned int `json:"allocations_planned"`
}

// IsHealthy reports whether the node is currently usable for placement
// and load reporting: status=="ready" AND its last heartbeat is recent.
// Pass time.Now() at the call site so callers driving large loops (e.g.
// snapshot builds) can reuse a single timestamp.
func (n *NodeInfo) IsHealthy(at time.Time) bool {
	return n.Status == "ready" && at.Sub(n.LastSeen) < nodeHeartbeatStaleAfter
}
