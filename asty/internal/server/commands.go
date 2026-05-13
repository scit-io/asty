package server

import (
	"encoding/json"
	"fmt"
	"time"

	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
)

// agentStartCommandTimeout bounds how long we wait for an agent to ack a
// start command. Most starts take milliseconds; we allow a generous margin
// for artifact downloads on first start.
const agentStartCommandTimeout = 30 * time.Second

// agentStopCommandTimeout is shorter — stops are local kills with no I/O.
const agentStopCommandTimeout = 5 * time.Second

// SendCommandToAgent sends an already-marshalled command to nodeID and
// returns the agent's response. Lower-level helpers below build on this.
func (s *Server) SendCommandToAgent(nodeID string, command []byte, timeout time.Duration) (*types.CommandResponse, error) {
	subject := fmt.Sprintf("asty.v1.agent.%s.cmd", nodeID)

	msg, err := s.nc.Request(subject, command, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to send command: %w", err)
	}

	var resp types.CommandResponse
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	return &resp, nil
}

// sendStartCommand provisions any declared KV buckets, injects their
// env vars into svc, then asks the agent to start the service.
func (s *Server) sendStartCommand(nodeID string, svc *types.ServiceDefinition) error {
	kvEnv, err := s.provisionKVBuckets(svc)
	if err != nil {
		return fmt.Errorf("provision KV: %w", err)
	}
	kvEnvForAllocation(svc, kvEnv)

	cmd, err := types.MarshalStartCommand(svc)
	if err != nil {
		return fmt.Errorf("failed to marshal start command: %w", err)
	}
	resp, err := s.SendCommandToAgent(nodeID, cmd, agentStartCommandTimeout)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("agent rejected start: %s", resp.Error)
	}
	return nil
}

// StopServiceOnNode dispatches a stop command to a node's agent.
func (s *Server) StopServiceOnNode(nodeID, serviceName string) error {
	cmd, err := types.MarshalStopCommand(serviceName)
	if err != nil {
		return fmt.Errorf("failed to marshal stop command: %w", err)
	}
	resp, err := s.SendCommandToAgent(nodeID, cmd, agentStopCommandTimeout)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("agent rejected stop: %s", resp.Error)
	}
	log.Info().
		Str("service", serviceName).
		Str("node_id", nodeID).
		Msg("stop command dispatched")
	return nil
}
