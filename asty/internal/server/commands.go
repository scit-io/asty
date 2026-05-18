package server

import (
	"fmt"
	"time"

	"asty/asty/internal/core/codec"
	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
)

// agentStartCommandTimeout bounds how long we wait for an agent to ack a
// start command. Most starts take milliseconds; we allow a generous margin
// for artifact downloads on first start.
const agentStartCommandTimeout = 30 * time.Second

// agentStopCommandTimeout is shorter — stops are local kills with no I/O.
const agentStopCommandTimeout = 5 * time.Second

// SendCommandToAgent sends a kind-typed command to nodeID and returns
// the agent's response. The subject embeds the kind (see
// types.CommandSubject) so the payload is plain — no envelope, no
// discriminator.
func (s *Server) SendCommandToAgent(nodeID string, kind types.CommandKind, command []byte, timeout time.Duration) (*types.CommandResponse, error) {
	subject := types.CommandSubject(nodeID, kind)

	msg, err := s.nc.Request(subject, command, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to send command: %w", err)
	}

	var resp types.CommandResponse
	if err := codec.Wire.Unmarshal(msg.Data, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	return &resp, nil
}

// sendStartCommand provisions any declared KV buckets, injects their
// env vars into svc, resolves ${VERSION}/${ARCH}/${GITHUB_REPO} in the
// artifact URL against this alloc's version, then asks the agent to
// start the service.
func (s *Server) sendStartCommand(nodeID string, svc *types.ServiceDefinition) error {
	kvEnv, err := s.provisionKVBuckets(svc)
	if err != nil {
		return fmt.Errorf("provision KV: %w", err)
	}
	kvEnvForAllocation(svc, kvEnv)

	resolved := s.resolvedSvcForDispatch(nodeID, svc, "")

	cmd, err := types.MarshalStartCommand(resolved)
	if err != nil {
		return fmt.Errorf("failed to marshal start command: %w", err)
	}
	resp, err := s.SendCommandToAgent(nodeID, types.CmdStart, cmd, agentStartCommandTimeout)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("agent rejected start: %s", resp.Error)
	}
	return nil
}

// sendRestartCommand asks the agent to stop the running copy and start a
// fresh one from the resolved svc. Used by the deployer to apply a new
// version: the dispatched svc has its artifact URL pre-expanded with
// version so the agent downloads the new tarball. Payload shape is
// identical to start; only the subject suffix differs.
func (s *Server) sendRestartCommand(nodeID string, svc *types.ServiceDefinition, version string) error {
	kvEnv, err := s.provisionKVBuckets(svc)
	if err != nil {
		return fmt.Errorf("provision KV: %w", err)
	}
	kvEnvForAllocation(svc, kvEnv)

	resolved := s.resolvedSvcForDispatch(nodeID, svc, version)

	cmd, err := types.MarshalStartCommand(resolved)
	if err != nil {
		return fmt.Errorf("failed to marshal restart command: %w", err)
	}
	resp, err := s.SendCommandToAgent(nodeID, types.CmdRestart, cmd, agentStartCommandTimeout)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("agent rejected restart: %s", resp.Error)
	}
	return nil
}

// resolvedSvcForDispatch returns a shallow copy of svc with the artifact
// URL placeholders expanded against the version associated with this
// dispatch. Caller-supplied version wins; falls back to alloc.Version
// from KV ("latest" if alloc is missing or unversioned).
func (s *Server) resolvedSvcForDispatch(nodeID string, svc *types.ServiceDefinition, version string) *types.ServiceDefinition {
	if version == "" {
		if alloc, err := s.clusterState.GetAllocation(svc.Name, nodeID); err == nil && alloc.Version != "" {
			version = alloc.Version
		}
	}
	if version == "" {
		version = "latest"
	}
	resolved := *svc
	resolved.Artifact.URL = s.resolveArtifactURL(svc.Artifact.URL, version)
	return &resolved
}

// StopServiceOnNode dispatches a stop command to a node's agent.
func (s *Server) StopServiceOnNode(nodeID, serviceName string) error {
	cmd, err := types.MarshalStopCommand(serviceName)
	if err != nil {
		return fmt.Errorf("failed to marshal stop command: %w", err)
	}
	resp, err := s.SendCommandToAgent(nodeID, types.CmdStop, cmd, agentStopCommandTimeout)
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
