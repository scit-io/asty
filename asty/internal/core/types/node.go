package types

import "time"

// nodeHeartbeatStaleAfter is the heartbeat-age threshold beyond which a
// node is considered unhealthy even if it last reported "ready". The agent
// publishes a heartbeat every 5 s, so 2 min is a wide safety margin that
// tolerates GC pauses and brief NATS reconnects without removing the node.
const nodeHeartbeatStaleAfter = 2 * time.Minute

// NodeStatus is the operator-visible state of a cluster node. Like
// AllocationStatus, defined as a typed string so the compiler catches
// stray literals.
type NodeStatus string

const (
	// NodeReady — accepting new allocations.
	NodeReady NodeStatus = "ready"

	// NodeDraining — actively being emptied by drain; existing
	// allocations migrate to peers.
	NodeDraining NodeStatus = "draining"

	// NodeDrained — drain finished; no allocations left.
	NodeDrained NodeStatus = "drained"

	// NodePaused — operator-set: existing allocations keep running,
	// but the scheduler will not place new ones here. Unpause by
	// setting status back to NodeReady via the API.
	NodePaused NodeStatus = "paused"

	// NodeDown — heartbeat went stale long enough to be considered
	// gone. Will not host new allocations.
	NodeDown NodeStatus = "down"

	// NodeDeleted — synthetic marker emitted by the state-watcher
	// for KV delete/purge events. Never persisted.
	NodeDeleted NodeStatus = "deleted"
)

// DiskType is the physical class of the disk hosting work_dir. Typed
// string so the wire format stays human-readable and JSON-stable
// while the compiler catches stray literals.
type DiskType string

const (
	// DiskUnknown — agent couldn't determine the disk class
	// (no /sys/block on Linux, diskutil failed on macOS, etc.).
	DiskUnknown DiskType = "unknown"

	// DiskSSD — solid-state device (NVMe, SATA SSD, eMMC).
	DiskSSD DiskType = "ssd"

	// DiskHDD — rotational (spinning) drive.
	DiskHDD DiskType = "hdd"
)

// NodeInfo represents information about a cluster node.
type NodeInfo struct {
	ID         string     `json:"id"`
	Datacenter string     `json:"datacenter"`
	IP         string     `json:"ip"`
	Status     NodeStatus `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeen   time.Time  `json:"last_seen"`

	// Resources
	CPUTotal        int   `json:"cpu_total"`        // MHz
	CPUAvailable    int   `json:"cpu_available"`
	MemoryTotal     int64 `json:"memory_total"`     // MB
	MemoryAvailable int64 `json:"memory_available"`
	DiskTotal       int64 `json:"disk_total"`       // MB, capacity of the filesystem hosting work_dir
	DiskAvailable   int64 `json:"disk_available"`   // MB
	DiskType        DiskType `json:"disk_type"`     // ssd | hdd | unknown
	SwapTotal       int64 `json:"swap_total"`       // MB
	SwapAvailable   int64 `json:"swap_available"`   // MB

	// Self — resource use of the asty agent process itself on this node.
	// Sampled by the agent so the UI can show "Asty CPU/RAM/Disk" tiles
	// separately from the services it manages.
	SelfCPUPercent float64 `json:"self_cpu_percent"`
	SelfMemoryMB   int64   `json:"self_memory_mb"`
	SelfDiskMB     int64   `json:"self_disk_mb"` // bytes-on-disk of work_dir (binaries + logs)

	// NATS — stats scraped from the local NATS server's monitoring port.
	// Zero values mean monitoring is unreachable; the UI/dashboards
	// should render "N/A" rather than literal zeros in that case.
	NATSCPUPercent        float64 `json:"nats_cpu_percent"`
	NATSMemoryMB          int64   `json:"nats_memory_mb"`
	NATSConnections       int     `json:"nats_connections"`
	NATSSubscriptions     int     `json:"nats_subscriptions"`
	NATSSlowConsumers     int64   `json:"nats_slow_consumers"`
	NATSInMsgs            int64   `json:"nats_in_msgs"`  // monotonic counter
	NATSOutMsgs           int64   `json:"nats_out_msgs"` // monotonic counter
	NATSJetStreamMessages int64   `json:"nats_jetstream_messages"`
	NATSJetStreamBytes    int64   `json:"nats_jetstream_bytes"` // JetStream on-disk size

	// Processes
	Processes []string `json:"processes"` // list of service names

	// Allocations counters (computed, not persisted in KV)
	AllocationsRunning int `json:"allocations_running"`
	AllocationsPlanned int `json:"allocations_planned"`
}

// IsHealthy reports whether the node is currently usable for placement
// and load reporting: status==NodeReady AND its last heartbeat is recent.
// Pass time.Now() at the call site so callers driving large loops (e.g.
// snapshot builds) can reuse a single timestamp.
func (n *NodeInfo) IsHealthy(at time.Time) bool {
	return n.Status == NodeReady && at.Sub(n.LastSeen) < nodeHeartbeatStaleAfter
}
