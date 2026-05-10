package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"asty/internal/platform/asty/core/config"
	"asty/internal/platform/asty/core/netutil"
	"asty/internal/platform/asty/core/types"
	autometrics "asty/internal/platform/asty/features/autoscaling/metrics"
	"asty/internal/platform/asty/features/clustering/leader"
	"asty/internal/platform/asty/features/clustering/state"
	"asty/internal/platform/asty/features/deployment"
	"asty/internal/platform/asty/features/draining"
	"asty/internal/platform/asty/features/observability/logs"
	"asty/internal/platform/asty/features/scheduling"

	apiPkg "asty/internal/platform/asty/features/api"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// --- ServerContext implementation (features/api.ServerContext) ---

func (s *Server) ClusterState() *state.ClusterState         { return s.clusterState }
func (s *Server) Services() []*types.ServiceDefinition      { return s.services }
func (s *Server) Config() *config.Config                    { return s.cfg }
func (s *Server) LeaderElection() *leader.Election          { return s.leaderElection }
func (s *Server) MetricsStore() *autometrics.Store          { return s.metricsStore }
func (s *Server) LogBuffer() *logs.Buffer                   { return s.logBuffer }
func (s *Server) EventBuffer() apiPkg.EventBufferReader     { return s.eventBuffer }
func (s *Server) DrainManager() *draining.DrainManager      { return s.drainManager }
func (s *Server) Deployer() *deployment.Deployer            { return s.deployer }
func (s *Server) NATSConn() *nats.Conn                      { return s.nc }
func (s *Server) StreamHub() apiPkg.StreamHub               { return s.streamHub }

// DeployService initiates a service deployment.
func (s *Server) DeployService(ctx context.Context, serviceName, version string) (*deployment.DeploymentStatus, error) {
	svc, err := s.serviceLoader.GetService(serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to load service definition: %w", err)
	}

	allocs, err := s.clusterState.ListAllocations(serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to list allocations: %w", err)
	}

	if len(allocs) == 0 {
		return nil, fmt.Errorf("no allocations found for service %s", serviceName)
	}

	currentVersion := "unknown"
	if len(allocs) > 0 {
		currentVersion = allocs[0].Version
	}

	plan := &deployment.DeploymentPlan{
		ServiceName:    serviceName,
		CurrentVersion: currentVersion,
		TargetVersion:  version,
		Allocations:    allocs,
		UpdateStrategy: deployment.UpdateStrategy{
			MaxParallel:      svc.Update.MaxParallel,
			MinHealthyTime:   parseUpdateDuration(svc.Update.MinHealthyTime, 10*time.Second),
			HealthyDeadline:  parseUpdateDuration(svc.Update.HealthyDeadline, 3*time.Minute),
			ProgressDeadline: parseUpdateDuration(svc.Update.ProgressDeadline, 10*time.Minute),
			AutoRevert:       svc.Update.AutoRevert,
			Canary:           1,
		},
		HealthyDeadline: parseUpdateDuration(svc.Update.HealthyDeadline, 3*time.Minute),
		MinHealthyTime:  parseUpdateDuration(svc.Update.MinHealthyTime, 10*time.Second),
		MaxParallel:     svc.Update.MaxParallel,
		AutoRevert:      svc.Update.AutoRevert,
		Canary:          1,
	}

	return s.deployer.Deploy(ctx, plan)
}

// --- DrainDeps interface implementation ---

func (s *Server) GetClusterState() *state.ClusterState    { return s.clusterState }
func (s *Server) GetScheduler() *scheduling.Scheduler     { return s.scheduler }
func (s *Server) GetServices() []*types.ServiceDefinition { return s.services }
func (s *Server) GetNATSConn() *nats.Conn                 { return s.nc }

// --- NATS ---

// connectNATS opens the NATS connection used by the server.
func (s *Server) connectNATS() error {
	nc, err := netutil.ConnectNATS(netutil.NATSCreds{
		Host: s.cfg.NATSHost, Port: s.cfg.NATSPort,
		User: s.cfg.NATSUser, Password: s.cfg.NATSPassword,
	}, "asty-server-"+s.nodeID)
	if err != nil {
		return err
	}
	s.nc = nc
	return nil
}

// --- Commands ---

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

// sendStartCommand asks an agent to start svc on its node and returns the
// first error it sees: marshal, transport, or agent-reported failure.
func (s *Server) sendStartCommand(nodeID string, svc *types.ServiceDefinition) error {
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

// --- Leadership ---

// watchLeadership re-arms the leader loop on flips.
func (s *Server) watchLeadership(ctx context.Context) {
	err := s.leaderElection.WatchLeadership(ctx,
		func() {
			log.Info().Msg("became leader")
			s.startLeaderWork(ctx)
		},
		func() {
			log.Info().Msg("lost leadership")
			s.stopLeaderWork()
		},
	)
	if err != nil {
		log.Error().Err(err).Msg("leadership watcher failed")
	}
}

// watchClusterNodes watches for cluster node changes via DNS.
func (s *Server) watchClusterNodes(ctx context.Context) {
	s.nodeDiscovery.WatchNodes(ctx, func(nodes []string) {
		log.Info().
			Strs("nodes", nodes).
			Int("count", len(nodes)).
			Msg("cluster nodes updated")
	})
}

// --- Log buffering ---

// startLogBuffering subscribes to NATS log subjects and feeds lines into
// logBuffer so that history endpoints can serve recent logs without a live SSE
// connection.
func (s *Server) startLogBuffering() {
	parseAndAppend := func(source string, data []byte) {
		var entry map[string]interface{}
		if err := json.Unmarshal(data, &entry); err != nil {
			return
		}

		level, _ := entry["level"].(string)
		msg, _ := entry["message"].(string)

		var ts int64
		switch v := entry["timestamp"].(type) {
		case float64:
			ts = int64(v)
		}

		timeStr := ""
		if t, ok := entry["time"].(string); ok {
			timeStr = t
		} else if ts > 0 {
			timeStr = fmt.Sprintf("%d", ts)
		}

		line := fmt.Sprintf("[%s] [%s] %s", timeStr, level, msg)
		s.logBuffer.Append(source, logs.LogLine{Timestamp: ts, Level: level, Line: line})
	}

	if _, err := s.nc.Subscribe("asty.v1.server.logs", func(msg *nats.Msg) {
		parseAndAppend("cluster", msg.Data)
	}); err != nil {
		log.Error().Err(err).Msg("failed to subscribe cluster log buffer")
	}

	// Subject pattern: asty.v1.agent.{nodeID}.logs.{service|"agent"}
	if _, err := s.nc.Subscribe("asty.v1.agent.*.logs.*", func(msg *nats.Msg) {
		parts := strings.Split(msg.Subject, ".")
		if len(parts) < 6 {
			return
		}
		nodeID := parts[3]
		svc := parts[5]
		if svc == "agent" {
			parseAndAppend("node."+nodeID, msg.Data)
		} else {
			parseAndAppend("node."+nodeID+".svc."+svc, msg.Data)
		}
	}); err != nil {
		log.Error().Err(err).Msg("failed to subscribe agent log buffer")
	}
}

// --- Metrics ---

// subscribeGatewayMetrics ingests per-gateway valid-RPS reports.
func (s *Server) subscribeGatewayMetrics() {
	subject := "asty.v1.metrics.gateway.*"
	_, err := s.nc.Subscribe(subject, func(msg *nats.Msg) {
		var report struct {
			NodeID   string  `json:"node_id"`
			ValidRPS float64 `json:"valid_rps"`
		}
		if err := json.Unmarshal(msg.Data, &report); err != nil {
			return
		}
		s.metricsStore.AddRPS(report.NodeID, report.ValidRPS)
	})
	if err != nil {
		log.Error().Err(err).Str("subject", subject).Msg("failed to subscribe to gateway metrics")
	}
}

// --- Utilities ---

func parseUpdateDuration(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}

// Compile-time interface checks.
var (
	_ apiPkg.ServerContext = (*Server)(nil)
	_ draining.DrainDeps  = (*Server)(nil)
)
