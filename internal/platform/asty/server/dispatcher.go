package server

import "asty/internal/platform/asty/core/types"

// serverDispatcher is the CommandDispatcher adapter that lets the controller
// reach into Server's NATS request/reply path without taking a *Server
// reference (would create a circular dependency between controller logic
// and Server lifecycle).
type serverDispatcher struct{ s *Server }

func (d serverDispatcher) SendStartCommand(nodeID string, svc *types.ServiceDefinition) error {
	return d.s.sendStartCommand(nodeID, svc)
}
