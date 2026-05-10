package types

import "encoding/json"

// Command represents a command sent from server to agent
type Command struct {
	Type string          `json:"type"` // start, stop, restart
	Data json.RawMessage `json:"data"`
}

// StartServiceCommand instructs agent to start a service
type StartServiceCommand struct {
	Service *ServiceDefinition `json:"service"`
}

// StopServiceCommand instructs agent to stop a service
type StopServiceCommand struct {
	ServiceName string `json:"service_name"`
}

// GetLogsCommand instructs agent to retrieve service logs
type GetLogsCommand struct {
	ServiceName string `json:"service_name"`
	Lines       int    `json:"lines"`
	Follow      bool   `json:"follow"`
}

// LogsResponse contains service logs
type LogsResponse struct {
	Success bool     `json:"success"`
	Error   string   `json:"error,omitempty"`
	Logs    []string `json:"logs"`
}

// CommandResponse represents agent's response to a command
type CommandResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}
