package discovery

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// dlog returns the global logger tagged as the discovery component.
// Fresh per call so writer/level reassignments (logs.AttachNATS,
// logs.SetLevel) take effect immediately — no init-time capture of the
// pre-configured global.
func dlog() *zerolog.Logger {
	l := log.With().Str("component", "discovery").Logger()
	return &l
}

// NodeDiscovery handles discovering cluster nodes via DNS
type NodeDiscovery struct {
	domain        string
	retryInterval time.Duration
}

// New creates a new node discovery instance
func New(domain string) *NodeDiscovery {
	return &NodeDiscovery{
		domain:        domain,
		retryInterval: 15 * time.Second,
	}
}

// DiscoverNodes resolves DNS A records to find cluster nodes
func (nd *NodeDiscovery) DiscoverNodes(ctx context.Context) ([]string, error) {
	ips, err := net.LookupIP(nd.domain)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", nd.domain, err)
	}

	nodes := make([]string, 0, len(ips))
	for _, ip := range ips {
		if ip.To4() != nil {
			nodes = append(nodes, ip.String())
		}
	}

	dlog().Info().
		Str("domain", nd.domain).
		Strs("nodes", nodes).
		Msg("discovered cluster nodes")

	return nodes, nil
}

// WatchNodes continuously monitors DNS for node changes
func (nd *NodeDiscovery) WatchNodes(ctx context.Context, onChange func([]string)) {
	ticker := time.NewTicker(nd.retryInterval)
	defer ticker.Stop()

	var previousNodes []string

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			nodes, err := nd.DiscoverNodes(ctx)
			if err != nil {
				dlog().Warn().Err(err).Str("domain", nd.domain).Msg("failed to discover nodes")
				continue
			}

			if nodesChanged(previousNodes, nodes) {
				dlog().Info().
					Strs("previous", previousNodes).
					Strs("current", nodes).
					Msg("cluster nodes changed")

				onChange(nodes)
				previousNodes = nodes
			}
		}
	}
}

func nodesChanged(a, b []string) bool {
	if len(a) != len(b) {
		return true
	}

	aMap := make(map[string]bool)
	for _, node := range a {
		aMap[node] = true
	}

	for _, node := range b {
		if !aMap[node] {
			return true
		}
	}

	return false
}
