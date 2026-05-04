package asty

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// ProximityMatrix manages datacenter latency information
type ProximityMatrix struct {
	latencies map[string]map[string]int // dc1 -> dc2 -> latency (ms)
}

// NewProximityMatrix creates a new proximity matrix
func NewProximityMatrix() *ProximityMatrix {
	return &ProximityMatrix{
		latencies: make(map[string]map[string]int),
	}
}

// LoadFromConfig loads latency matrix from config string
// Format: "dc1:dc2:100,dc1:dc3:250,dc2:dc3:200"
func (pm *ProximityMatrix) LoadFromConfig(config string) error {
	if config == "" {
		return nil
	}

	pairs := strings.Split(config, ",")
	for _, pair := range pairs {
		parts := strings.Split(strings.TrimSpace(pair), ":")
		if len(parts) != 3 {
			return fmt.Errorf("invalid latency format: %s (expected dc1:dc2:latency)", pair)
		}

		dc1 := parts[0]
		dc2 := parts[1]
		var latency int
		if _, err := fmt.Sscanf(parts[2], "%d", &latency); err != nil {
			return fmt.Errorf("invalid latency value: %s", parts[2])
		}

		pm.SetLatency(dc1, dc2, latency)
	}

	log.Info().
		Int("entries", len(pairs)).
		Msg("loaded latency matrix from config")

	return nil
}

// SetLatency sets the latency between two datacenters (bidirectional)
func (pm *ProximityMatrix) SetLatency(dc1, dc2 string, latencyMs int) {
	if pm.latencies[dc1] == nil {
		pm.latencies[dc1] = make(map[string]int)
	}
	if pm.latencies[dc2] == nil {
		pm.latencies[dc2] = make(map[string]int)
	}

	pm.latencies[dc1][dc2] = latencyMs
	pm.latencies[dc2][dc1] = latencyMs
}

// GetLatency returns the latency between two datacenters
func (pm *ProximityMatrix) GetLatency(dc1, dc2 string) (int, bool) {
	if dc1 == dc2 {
		return 0, true
	}

	if pm.latencies[dc1] != nil {
		if latency, exists := pm.latencies[dc1][dc2]; exists {
			return latency, true
		}
	}

	return 0, false
}

// GetNearestDatacenter returns the nearest datacenter to the source
func (pm *ProximityMatrix) GetNearestDatacenter(source string, candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}

	var nearest string
	minLatency := int(^uint(0) >> 1) // max int

	for _, dc := range candidates {
		if dc == source {
			return dc // Same DC is always nearest
		}

		latency, exists := pm.GetLatency(source, dc)
		if !exists {
			continue
		}

		if latency < minLatency {
			minLatency = latency
			nearest = dc
		}
	}

	// If no latency info, return first candidate
	if nearest == "" && len(candidates) > 0 {
		return candidates[0]
	}

	return nearest
}

// SortDatacentersByProximity sorts datacenters by proximity to source
func (pm *ProximityMatrix) SortDatacentersByProximity(source string, dcs []string) []string {
	type dcLatency struct {
		name    string
		latency int
	}

	dcList := make([]dcLatency, 0, len(dcs))
	for _, dc := range dcs {
		if dc == source {
			// Same DC always first
			return append([]string{dc}, pm.SortDatacentersByProximity(source, removeFromSlice(dcs, dc))...)
		}

		latency, exists := pm.GetLatency(source, dc)
		if !exists {
			latency = 1000 // Unknown latency - deprioritize
		}

		dcList = append(dcList, dcLatency{name: dc, latency: latency})
	}

	// Sort by latency
	for i := 0; i < len(dcList)-1; i++ {
		for j := i + 1; j < len(dcList); j++ {
			if dcList[j].latency < dcList[i].latency {
				dcList[i], dcList[j] = dcList[j], dcList[i]
			}
		}
	}

	result := make([]string, len(dcList))
	for i, dc := range dcList {
		result[i] = dc.name
	}

	return result
}

// ValidateLatencies pings nodes to validate configured latencies
func (pm *ProximityMatrix) ValidateLatencies(ctx context.Context, nodes []*NodeInfo) {
	// Group nodes by datacenter
	dcNodes := make(map[string][]*NodeInfo)
	for _, node := range nodes {
		dc := node.Datacenter
		if dc == "" {
			dc = "default"
		}
		dcNodes[dc] = append(dcNodes[dc], node)
	}

	// For each DC pair with configured latency, validate
	for dc1, targets := range pm.latencies {
		for dc2, configuredLatency := range targets {
			if dc1 >= dc2 {
				continue // Skip duplicate pairs
			}

			// Measure actual latency
			actualLatency := pm.measureLatency(dcNodes[dc1], dcNodes[dc2])
			if actualLatency == 0 {
				continue // No nodes to measure
			}

			// Check for significant divergence (>50%)
			divergence := float64(actualLatency-configuredLatency) / float64(configuredLatency)
			if divergence > 0.5 || divergence < -0.5 {
				log.Warn().
					Str("dc1", dc1).
					Str("dc2", dc2).
					Int("configured", configuredLatency).
					Int("actual", actualLatency).
					Float64("divergence", divergence*100).
					Msg("latency divergence detected")
			} else {
				log.Debug().
					Str("dc1", dc1).
					Str("dc2", dc2).
					Int("configured", configuredLatency).
					Int("actual", actualLatency).
					Msg("latency validated")
			}
		}
	}
}

// measureLatency measures average latency between two DC node groups
func (pm *ProximityMatrix) measureLatency(nodes1, nodes2 []*NodeInfo) int {
	if len(nodes1) == 0 || len(nodes2) == 0 {
		return 0
	}

	// Pick first node from each DC and ping
	node1 := nodes1[0]
	node2 := nodes2[0]

	if node1.IP == "" || node2.IP == "" {
		return 0
	}

	latency := pm.pingNode(node1.IP)
	if latency == 0 {
		return 0
	}

	return latency
}

// pingNode measures ICMP ping latency to a node
func (pm *ProximityMatrix) pingNode(ip string) int {
	// Simple TCP dial as ping approximation
	// For production, use ICMP ping library
	start := time.Now()
	conn, err := net.DialTimeout("tcp", ip+":4646", 2*time.Second)
	if err != nil {
		return 0
	}
	defer conn.Close()

	latency := time.Since(start).Milliseconds()
	return int(latency)
}

// removeFromSlice removes an element from a string slice
func removeFromSlice(slice []string, item string) []string {
	result := make([]string, 0, len(slice)-1)
	for _, s := range slice {
		if s != item {
			result = append(result, s)
		}
	}
	return result
}

// RunValidation runs periodic latency validation
func (pm *ProximityMatrix) RunValidation(ctx context.Context, clusterState *ClusterState) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			nodes, err := clusterState.ListNodes()
			if err != nil {
				log.Error().Err(err).Msg("failed to list nodes for latency validation")
				continue
			}

			pm.ValidateLatencies(ctx, nodes)
		}
	}
}
