package types

import "time"

// ServiceAllocation represents a service instance placement
type ServiceAllocation struct {
	ID           string    `json:"id"`
	ServiceName  string    `json:"service_name"`
	NodeID       string    `json:"node_id"`
	Status       string    `json:"status"` // pending, running, stopped, failed
	Version      string    `json:"version"`
	PID          int       `json:"pid"`
	StartedAt    time.Time `json:"started_at"`
	HealthStatus string    `json:"health_status"` // healthy, unhealthy, unknown
	CPUUsage     int       `json:"cpu_usage"`     // Percentage
	MemoryUsage  int       `json:"memory_usage"`  // MB
	Restarts            int       `json:"restarts"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ServiceCooldown captures the timestamps of the most recent autoscaler
// actions for a service.
type ServiceCooldown struct {
	LastScaleUp   time.Time `json:"last_scale_up,omitempty"`
	LastScaleDown time.Time `json:"last_scale_down,omitempty"`
}
