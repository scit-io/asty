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

// agentShutdownCommandTimeout — only the ack; agent runs graceful
// shutdown async after acking.
const agentShutdownCommandTimeout = 10 * time.Second

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

	resolved := s.resolvedSvcForDispatch(nodeID, svc, "")
	kvEnvForAllocation(resolved, kvEnv)

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
// version and by RestartServiceOnNode for the dashboard's per-allocation
// restart. The dispatched svc has its artifact URL pre-expanded with
// version so the agent downloads the new tarball. Payload shape is
// identical to start; only the subject suffix differs.
//
// The RPC timeout is sized to kill_timeout + the regular start budget
// because the agent runs the restart synchronously: SIGTERM, wait up to
// kill_timeout, optional SIGKILL, then the start sequence (artifact
// download + spawn). Using agentStartCommandTimeout alone is too tight
// once kill_timeout reaches its default of 30 s.
func (s *Server) sendRestartCommand(nodeID string, svc *types.ServiceDefinition, version string) error {
	kvEnv, err := s.provisionKVBuckets(svc)
	if err != nil {
		return fmt.Errorf("provision KV: %w", err)
	}

	resolved := s.resolvedSvcForDispatch(nodeID, svc, version)
	kvEnvForAllocation(resolved, kvEnv)

	cmd, err := types.MarshalStartCommand(resolved)
	if err != nil {
		return fmt.Errorf("failed to marshal restart command: %w", err)
	}
	timeout := svc.GetKillTimeout() + agentStartCommandTimeout
	resp, err := s.SendCommandToAgent(nodeID, types.CmdRestart, cmd, timeout)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("agent rejected restart: %s", resp.Error)
	}
	return nil
}

// RestartServiceOnNode dispatches a synchronous in-place restart of
// serviceName on nodeID. Resolves the current service definition from
// the loader so artifact URL placeholders pick up the latest pinned
// version. Returns when the agent confirms the new copy is up, or with
// an error on dispatch or restart failure.
func (s *Server) RestartServiceOnNode(nodeID, serviceName string) error {
	svc, err := s.serviceLoader.GetService(serviceName)
	if err != nil {
		return fmt.Errorf("load service: %w", err)
	}
	return s.sendRestartCommand(nodeID, svc, "")
}

// resolvedSvcForDispatch returns a per-dispatch copy of svc with the
// artifact URL placeholders expanded against this dispatch's version and
// the Env / KV / Resources maps deep-copied so the caller can mutate them
// (kvEnvForAllocation injects per-bucket env vars) without racing the
// shared ServiceDefinition the loader hands out — the same definition is
// reused across reconciler workers, the dashboard restart path, and the
// deployer, all of which can dispatch concurrently. Without this copy a
// concurrent CBOR Marshal of svc.Env (during MarshalStartCommand) and an
// in-place kvEnvForAllocation write to the same map crashes the server
// with "fatal: concurrent map iteration and map write".
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
	if svc.Env != nil {
		resolved.Env = make(map[string]string, len(svc.Env))
		for k, v := range svc.Env {
			resolved.Env[k] = v
		}
	}
	return &resolved
}

// ShutdownAgent asks the agent on nodeID to begin a graceful self-shutdown,
// then evicts that node from the JetStream meta group right away so the meta
// shrinks WITH the kill. NATS marks a departed peer offline only by a slow
// RAFT timeout; a fast multi-node shrink outruns that detection, so without an
// explicit eviction the killed peers pile up in the meta until it loses quorum
// (no meta leader, unrecoverable). The leader issues the removal — it is not
// the node leaving, so it cannot stall itself — and the lazy detection-based
// reaper (deadpeers.go) stays as the fallback for ungraceful crashes.
func (s *Server) ShutdownAgent(nodeID string) error {
	resp, err := s.SendCommandToAgent(nodeID, types.CmdShutdown, nil, agentShutdownCommandTimeout)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("agent rejected shutdown: %s", resp.Error)
	}
	log.Info().Str("node_id", nodeID).Msg("shutdown command dispatched")
	// The agent is now stopping its nats-server; drop it from the meta group
	// immediately so quorum stays reachable through the shrink instead of
	// waiting on offline-detection.
	if s.ncSys != nil {
		s.removeNATSPeer(nodeID)
	}
	return nil
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
