package types

import "time"

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
