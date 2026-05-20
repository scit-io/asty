package types

import (
	"fmt"
	"strings"

	"asty/asty/internal/core/codec"
)

// CommandKind is the trailing segment of a per-command NATS subject.
// Discriminator lives in the subject, not in the payload — the agent
// subscribes to CommandPattern(nodeID) and routes on the suffix
// returned by CommandKindFromSubject, so encode and decode are each
// one Marshal/Unmarshal pass with no envelope.
type CommandKind string

const (
	CmdStart    CommandKind = "start"
	CmdRestart  CommandKind = "restart"
	CmdStop     CommandKind = "stop"
	CmdLogs     CommandKind = "logs"
	CmdShutdown CommandKind = "shutdown"
)

const (
	commandSubjectFormat = "asty.v1.agent.%s.cmd.%s"
	commandPatternFormat = "asty.v1.agent.%s.cmd.*"
)

// CommandSubject returns the NATS subject the server publishes a
// command of kind to for nodeID.
func CommandSubject(nodeID string, kind CommandKind) string {
	return fmt.Sprintf(commandSubjectFormat, nodeID, kind)
}

// CommandPattern returns the wildcard subscribe pattern the agent on
// nodeID uses to receive every command kind addressed to it.
func CommandPattern(nodeID string) string {
	return fmt.Sprintf(commandPatternFormat, nodeID)
}

// CommandKindFromSubject extracts the kind suffix from a subject built
// by CommandSubject. Returns empty string on malformed subjects.
func CommandKindFromSubject(subject string) CommandKind {
	i := strings.LastIndexByte(subject, '.')
	if i < 0 {
		return ""
	}
	return CommandKind(subject[i+1:])
}

// StartServiceCommand is the payload of CmdStart and CmdRestart — the
// kind alone distinguishes "start fresh" from "stop then start with
// this new svc def" (which deploy rolling-update uses to apply a new
// version), so the wire shape is identical.
type StartServiceCommand struct {
	Service *ServiceDefinition `json:"service"`
}

// StopServiceCommand is the payload of CmdStop.
type StopServiceCommand struct {
	ServiceName string `json:"service_name"`
}

// GetLogsCommand is the payload of CmdLogs.
type GetLogsCommand struct {
	ServiceName string `json:"service_name"`
	Lines       int    `json:"lines"`
	Follow      bool   `json:"follow"`
}

// LogsResponse is the agent reply for CmdLogs.
type LogsResponse struct {
	Success bool     `json:"success"`
	Error   string   `json:"error,omitempty"`
	Logs    []string `json:"logs"`
}

// CommandResponse is the agent reply for non-logs commands.
type CommandResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

// MarshalStartCommand encodes the payload for CmdStart or CmdRestart.
func MarshalStartCommand(svc *ServiceDefinition) ([]byte, error) {
	return codec.Wire.Marshal(StartServiceCommand{Service: svc})
}

// MarshalStopCommand encodes the payload for CmdStop.
func MarshalStopCommand(serviceName string) ([]byte, error) {
	return codec.Wire.Marshal(StopServiceCommand{ServiceName: serviceName})
}

// MarshalGetLogsCommand encodes the payload for CmdLogs.
func MarshalGetLogsCommand(serviceName string, lines int, follow bool) ([]byte, error) {
	return codec.Wire.Marshal(GetLogsCommand{ServiceName: serviceName, Lines: lines, Follow: follow})
}

// ParseStartCommand decodes a CmdStart or CmdRestart payload.
func ParseStartCommand(data []byte) (*StartServiceCommand, error) {
	var cmd StartServiceCommand
	if err := codec.Wire.Unmarshal(data, &cmd); err != nil {
		return nil, fmt.Errorf("parse start command: %w", err)
	}
	return &cmd, nil
}

// ParseStopCommand decodes a CmdStop payload.
func ParseStopCommand(data []byte) (*StopServiceCommand, error) {
	var cmd StopServiceCommand
	if err := codec.Wire.Unmarshal(data, &cmd); err != nil {
		return nil, fmt.Errorf("parse stop command: %w", err)
	}
	return &cmd, nil
}

// ParseGetLogsCommand decodes a CmdLogs payload.
func ParseGetLogsCommand(data []byte) (*GetLogsCommand, error) {
	var cmd GetLogsCommand
	if err := codec.Wire.Unmarshal(data, &cmd); err != nil {
		return nil, fmt.Errorf("parse get logs command: %w", err)
	}
	return &cmd, nil
}

// MarshalResponse builds the generic agent reply. Marshal failure is
// silently swallowed: the response types here can't fail to encode.
func MarshalResponse(success bool, message string, err error) []byte {
	resp := CommandResponse{Success: success, Message: message}
	if err != nil {
		resp.Error = err.Error()
	}
	data, _ := codec.Wire.Marshal(resp)
	return data
}

// MarshalLogsResponse builds the agent reply for CmdLogs.
func MarshalLogsResponse(logs []string, err error) []byte {
	resp := LogsResponse{Success: err == nil, Logs: logs}
	if err != nil {
		resp.Error = err.Error()
	}
	data, _ := codec.Wire.Marshal(resp)
	return data
}
