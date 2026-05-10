package asty

import (
	"encoding/json"
	"fmt"

	"asty/internal/platform/asty/core/types"
)

// Type aliases for backward compatibility
type Command = types.Command
type StartServiceCommand = types.StartServiceCommand
type StopServiceCommand = types.StopServiceCommand
type GetLogsCommand = types.GetLogsCommand
type LogsResponse = types.LogsResponse
type CommandResponse = types.CommandResponse

// MarshalStartCommand creates a start service command
func MarshalStartCommand(svc *ServiceDefinition) ([]byte, error) {
	cmd := StartServiceCommand{Service: svc}
	data, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}

	command := Command{
		Type: "start",
		Data: data,
	}

	return json.Marshal(command)
}

// MarshalStopCommand creates a stop service command
func MarshalStopCommand(serviceName string) ([]byte, error) {
	cmd := StopServiceCommand{ServiceName: serviceName}
	data, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}

	command := Command{
		Type: "stop",
		Data: data,
	}

	return json.Marshal(command)
}

// MarshalGetLogsCommand creates a get logs command
func MarshalGetLogsCommand(serviceName string, lines int, follow bool) ([]byte, error) {
	cmd := GetLogsCommand{
		ServiceName: serviceName,
		Lines:       lines,
		Follow:      follow,
	}
	data, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}

	command := Command{
		Type: "logs",
		Data: data,
	}

	return json.Marshal(command)
}

// UnmarshalCommand parses a command
func UnmarshalCommand(data []byte) (*Command, error) {
	var cmd Command
	if err := json.Unmarshal(data, &cmd); err != nil {
		return nil, fmt.Errorf("failed to unmarshal command: %w", err)
	}
	return &cmd, nil
}

// ParseStartCommand parses start service command data
func ParseStartCommand(data json.RawMessage) (*StartServiceCommand, error) {
	var cmd StartServiceCommand
	if err := json.Unmarshal(data, &cmd); err != nil {
		return nil, fmt.Errorf("failed to parse start command: %w", err)
	}
	return &cmd, nil
}

// ParseStopCommand parses stop service command data
func ParseStopCommand(data json.RawMessage) (*StopServiceCommand, error) {
	var cmd StopServiceCommand
	if err := json.Unmarshal(data, &cmd); err != nil {
		return nil, fmt.Errorf("failed to parse stop command: %w", err)
	}
	return &cmd, nil
}

// ParseGetLogsCommand parses get logs command data
func ParseGetLogsCommand(data json.RawMessage) (*GetLogsCommand, error) {
	var cmd GetLogsCommand
	if err := json.Unmarshal(data, &cmd); err != nil {
		return nil, fmt.Errorf("failed to parse get logs command: %w", err)
	}
	return &cmd, nil
}

// MarshalResponse creates a command response
func MarshalResponse(success bool, message string, err error) []byte {
	resp := CommandResponse{
		Success: success,
		Message: message,
	}

	if err != nil {
		resp.Error = err.Error()
	}

	data, _ := json.Marshal(resp)
	return data
}

// MarshalLogsResponse creates a logs response
func MarshalLogsResponse(logs []string, err error) []byte {
	resp := LogsResponse{
		Success: err == nil,
		Logs:    logs,
	}

	if err != nil {
		resp.Error = err.Error()
	}

	data, _ := json.Marshal(resp)
	return data
}
