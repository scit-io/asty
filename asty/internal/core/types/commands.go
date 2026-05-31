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
	// CmdAddPeer registers a NATS-route peer on the agent that receives
	// it. Carries just the peer's IP — node_id/host are filled in later
	// by the agent's own KV WatchNodes once the new node registers
	// itself (so we don't ask the operator to type them at bootstrap).
	CmdAddPeer CommandKind = "add_peer"
)

const (
	commandSubjectFormat = "asty.v1.agent.%s.cmd.%s"
	commandPatternFormat = "asty.v1.agent.%s.cmd.*"
)

// PeerAnnounceSubject is the cluster-wide topic on which every agent
// republishes a freshly-registered NATS-route peer (after a local
// CmdAddPeer). Every agent subscribes to it so a single SSH'd
// add-peer against any one node propagates the new IP to ALL nodes —
// the same full-mesh behaviour the DNS-A-record scheme used to give
// for free. Payload is the same AddPeerCommand shape.
const PeerAnnounceSubject = "asty.v1.cluster.peer_announce"

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

// AddPeerCommand is the payload of CmdAddPeer — just the peer's IP.
// Identity (node_id, host) is intentionally NOT in the payload: it
// flows in via the cluster KV once the new node registers itself.
type AddPeerCommand struct {
	IP string `json:"ip"`
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

// MarshalAddPeerCommand encodes the payload for CmdAddPeer.
func MarshalAddPeerCommand(ip string) ([]byte, error) {
	return codec.Wire.Marshal(AddPeerCommand{IP: ip})
}

// ParseAddPeerCommand decodes a CmdAddPeer payload.
func ParseAddPeerCommand(data []byte) (*AddPeerCommand, error) {
	var cmd AddPeerCommand
	if err := codec.Wire.Unmarshal(data, &cmd); err != nil {
		return nil, fmt.Errorf("parse add-peer command: %w", err)
	}
	return &cmd, nil
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
