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

// CooldownStatus describes which cooldowns are currently active and what
// the most-recent autoscaler action was. It mirrors the fields the API
// and the snapshot builder both surface to the UI.
type CooldownStatus struct {
	UpActive     bool   `json:"cooldown_up_active"`
	DownActive   bool   `json:"cooldown_down_active"`
	LastAction   string `json:"last_action"`     // "scale_up", "scale_down", or "" if none yet
	LastActionAt int64  `json:"last_action_at"`  // unix seconds
}

// Status reports cooldown state at time `at` given the configured
// scale-up/down windows. The result tells the UI which controls to disable
// (cooldown is active) and labels the most recent action so operators see
// "why nothing happened".
func (c ServiceCooldown) Status(at time.Time, up, down time.Duration) CooldownStatus {
	var s CooldownStatus
	if !c.LastScaleUp.IsZero() {
		s.UpActive = at.Sub(c.LastScaleUp) < up
		s.LastAction = "scale_up"
		s.LastActionAt = c.LastScaleUp.Unix()
	}
	if !c.LastScaleDown.IsZero() {
		s.DownActive = at.Sub(c.LastScaleDown) < down
		if c.LastScaleDown.Unix() > s.LastActionAt {
			s.LastAction = "scale_down"
			s.LastActionAt = c.LastScaleDown.Unix()
		}
	}
	return s
}
