package asty

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// serverDispatcher is the CommandDispatcher adapter that lets the controller
// reach into Server's NATS request/reply path without taking a *Server
// reference (would create a circular dependency between controller logic
// and Server lifecycle).
type serverDispatcher struct{ s *Server }

func (d serverDispatcher) SendStartCommand(nodeID string, svc *ServiceDefinition) error {
	return d.s.sendStartCommand(nodeID, svc)
}

// sendStartCommand sends a start command to an agent
func (s *Server) sendStartCommand(nodeID string, svc *ServiceDefinition) error {
	cmdBytes, err := MarshalStartCommand(svc)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	resp, err := s.SendCommandToAgent(nodeID, cmdBytes, 30*time.Second)
	if err != nil {
		return fmt.Errorf("failed to send command: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("agent returned error: %s", resp.Message)
	}

	return nil
}

// SendCommandToAgent sends a command to an agent
func (s *Server) SendCommandToAgent(nodeID string, command []byte, timeout time.Duration) (*CommandResponse, error) {
	subject := fmt.Sprintf("asty.v1.agent.%s.cmd", nodeID)

	msg, err := s.nc.Request(subject, command, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to send command: %w", err)
	}

	var resp CommandResponse
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &resp, nil
}

// StartServiceOnNode starts a service on a specific node
func (s *Server) StartServiceOnNode(nodeID string, svc *ServiceDefinition) error {
	cmd, err := MarshalStartCommand(svc)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	resp, err := s.SendCommandToAgent(nodeID, cmd, 30*time.Second)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("command failed: %s", resp.Error)
	}

	log.Info().
		Str("service", svc.Name).
		Str("node_id", nodeID).
		Msg("service started on node")

	return nil
}

// StopServiceOnNode dispatches a stop command to a node's agent. The agent
// acknowledges immediately and performs the actual shutdown asynchronously, so
// this call is short — only enough for NATS round-trip to the ack. Confirmation
// that the process has stopped comes from KV state (alloc.Status="stopped"),
// which the drain manager polls.
func (s *Server) StopServiceOnNode(nodeID, serviceName string) error {
	cmd, err := MarshalStopCommand(serviceName)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	resp, err := s.SendCommandToAgent(nodeID, cmd, 5*time.Second)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("command failed: %s", resp.Error)
	}

	log.Info().
		Str("service", serviceName).
		Str("node_id", nodeID).
		Msg("stop command dispatched")

	return nil
}

// DrainDeps interface implementation

func (s *Server) GetClusterState() *ClusterState       { return s.clusterState }
func (s *Server) GetScheduler() *Scheduler             { return s.scheduler }
func (s *Server) GetServices() []*ServiceDefinition    { return s.services }
func (s *Server) GetNATSConn() *nats.Conn              { return s.nc }

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

// watchClusterNodes watches for cluster node changes via DNS
func (s *Server) watchClusterNodes(ctx context.Context) {
	s.nodeDiscovery.WatchNodes(ctx, func(nodes []string) {
		log.Info().
			Strs("nodes", nodes).
			Int("count", len(nodes)).
			Msg("cluster nodes updated")
	})
}

// DeployService initiates a service deployment
func (s *Server) DeployService(ctx context.Context, serviceName, version string) (*DeploymentStatus, error) {
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

	plan := &DeploymentPlan{
		ServiceName:    serviceName,
		CurrentVersion: currentVersion,
		TargetVersion:  version,
		Allocations:    allocs,
		UpdateStrategy: UpdateStrategy{
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
		s.logBuffer.Append(source, LogLine{Timestamp: ts, Level: level, Line: line})
	}

	if _, err := s.nc.Subscribe("asty.v1.server.logs", func(msg *nats.Msg) {
		parseAndAppend("cluster", msg.Data)
	}); err != nil {
		log.Error().Err(err).Msg("failed to subscribe cluster log buffer")
	}

	// Subject pattern: asty.v1.agent.{nodeID}.logs.{service|"agent"}
	if _, err := s.nc.Subscribe("asty.v1.agent.*.logs.*", func(msg *nats.Msg) {
		parts := splitSubject(msg.Subject)
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

// connectNATS establishes connection to NATS
func (s *Server) connectNATS() error {
	natsURL := fmt.Sprintf("nats://%s:%s", s.cfg.NATSHost, s.cfg.NATSPort)

	opts := []nats.Option{
		nats.Name("asty-server-" + s.nodeID),
	}

	if s.cfg.NATSUser != "" {
		opts = append(opts, nats.UserInfo(s.cfg.NATSUser, s.cfg.NATSPassword))
	}

	nc, err := nats.Connect(natsURL, opts...)
	if err != nil {
		return err
	}

	s.nc = nc
	log.Info().Str("url", natsURL).Msg("connected to NATS")
	return nil
}

// splitSubject splits a NATS subject by '.'.
func splitSubject(subject string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(subject); i++ {
		if subject[i] == '.' {
			parts = append(parts, subject[start:i])
			start = i + 1
		}
	}
	parts = append(parts, subject[start:])
	return parts
}
