package asty

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/rs/zerolog/log"
)

// NodeDiscovery handles discovering cluster nodes via DNS
type NodeDiscovery struct {
	domain       string
	retryMax     int
	retryInterval time.Duration
}

// NewNodeDiscovery creates a new node discovery instance
func NewNodeDiscovery(domain string) *NodeDiscovery {
	return &NodeDiscovery{
		domain:       domain,
		retryMax:     0, // 0 = retry forever
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
		// Only IPv4 for now
		if ip.To4() != nil {
			nodes = append(nodes, ip.String())
		}
	}

	log.Info().
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
				log.Warn().Err(err).Msg("failed to discover nodes")
				continue
			}

			// Check if nodes changed
			if nodesChanged(previousNodes, nodes) {
				log.Info().
					Strs("previous", previousNodes).
					Strs("current", nodes).
					Msg("cluster nodes changed")

				onChange(nodes)
				previousNodes = nodes
			}
		}
	}
}

// nodesChanged checks if two node lists are different
func nodesChanged(a, b []string) bool {
	if len(a) != len(b) {
		return true
	}

	// Create map for quick lookup
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
