package agent

import (
	"net"

	"github.com/rs/zerolog/log"
)

// resolveNATSPeers returns the IPs of OTHER cluster nodes for the
// rendered cluster.routes, resolved from DNS A-records of cfg.Domain.
// Prod points the domain at the cluster's nodes; dev points asty.test
// at every node's loopback alias via /etc/hosts (start.sh's sync_hosts
// maintains the records). One discovery path for both environments.
//
// Self-IP is filtered so a node never routes to itself.
func (a *Agent) resolveNATSPeers(selfIP string) []string {
	if a.cfg.Domain == "" {
		return nil
	}
	ips, err := net.LookupIP(a.cfg.Domain)
	if err != nil {
		log.Warn().Err(err).Str("domain", a.cfg.Domain).Msg("NATS peer discovery: DNS lookup failed")
		return nil
	}
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		if ip.To4() == nil {
			continue
		}
		out = append(out, ip.String())
	}
	return filterSelf(out, selfIP)
}

// filterSelf removes selfIP from ips so a node never routes to itself.
func filterSelf(ips []string, self string) []string {
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		if ip != self {
			out = append(out, ip)
		}
	}
	return out
}
