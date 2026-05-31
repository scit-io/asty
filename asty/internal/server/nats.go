package server

import (
	"fmt"
	"strconv"
	"time"

	"asty/asty/internal/core/netutil"

	"github.com/nats-io/nats.go"
)

// pingProbeTimeout caps a single peer-to-peer ping round-trip. The agent's
// own inner probe (ping-peer → ping) uses its own 2s budget, so we add slack
// for the wrapping ping-peer hop here. Short enough that one unreachable
// peer can't stall the hourly validation pass.
const pingProbeTimeout = 3 * time.Second

// connectNATS opens the NATS connection used by the server. Thin wrapper
// over core/netutil — agent does the same so they share startup options.
// The DiscoveredServersHandler is wired here so the leader's stream-
// replicas reconcile fires on the earliest reliable "peer joined"
// signal (NATS gossip), not on the much-later KV WatchNodes event
// which can't fire until the joiner has bucket access — a chicken-and-
// egg that left streams stuck at the old replica count and bricked
// the joiner's bucket-init.
func (s *Server) connectNATS() error {
	host := s.cfg.NodeIP
	if host == "" {
		host = netutil.LocalIPv4("")
	}
	notify := func(*nats.Conn) {
		select {
		case s.gossipChanged <- struct{}{}:
		default:
		}
	}
	nc, err := netutil.ConnectNATS(netutil.NATSCreds{
		Host: host, Port: s.cfg.NATS.Server.Port,
		User: s.cfg.NATS.User, Password: s.cfg.NATS.Password,
	}, "asty-server-"+s.nodeID, nats.DiscoveredServersHandler(notify))
	if err != nil {
		return err
	}
	s.nc = nc
	return nil
}

// pingPair asks srcID's agent to ping tgtID's agent and returns the
// reported RTT in milliseconds, or 0 on any error. The path is
// server → srcID.ping-peer → tgtID.ping → srcID → server, so the
// measurement reflects srcID↔tgtID network latency (with a tiny
// NATS-dispatch overhead) rather than server↔srcID.
// Wired into proximity.RunValidation as a PingFn.
func (s *Server) pingPair(srcID, tgtID string) int {
	subject := fmt.Sprintf("asty.v1.agent.%s.ping-peer", srcID)
	msg, err := s.nc.Request(subject, []byte(tgtID), pingProbeTimeout)
	if err != nil {
		return 0
	}
	ms, err := strconv.Atoi(string(msg.Data))
	if err != nil || ms < 0 {
		return 0
	}
	return ms
}
