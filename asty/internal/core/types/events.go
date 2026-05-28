package types

import "time"

// ClusterEvent is a single cluster lifecycle event.
type ClusterEvent struct {
	Timestamp int64  `json:"ts"`
	Type      string `json:"type"` // scale_up, scale_down, alloc_failed, node_join, node_leave
	Service   string `json:"service,omitempty"`
	NodeID    string `json:"node_id,omitempty"`
	Details   string `json:"details,omitempty"`
}

func NewEvent(typ, service, nodeID, details string) ClusterEvent {
	return ClusterEvent{
		Timestamp: time.Now().Unix(),
		Type:      typ,
		Service:   service,
		NodeID:    nodeID,
		Details:   details,
	}
}
