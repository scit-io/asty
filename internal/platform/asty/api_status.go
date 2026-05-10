package asty

import (
	"fmt"
	"net/http"
	"time"
)

// handleRoot returns API information
func (api *API) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	response := map[string]interface{}{
		"service": "Asty Orchestrator",
		"version": "0.1.0",
		"api":     "/api/v1",
		"endpoints": map[string]string{
			"status":            "/api/v1/status",
			"nodes":             "/api/v1/nodes",
			"services":          "/api/v1/services",
			"allocations":       "/api/v1/allocations",
			"deploy":            "/api/v1/deploy",
			"deployments":       "/api/v1/deployments",
			"autoscaler_status": "/api/v1/autoscaler/status",
			"autoscaler_events": "/api/v1/autoscaler/events",
			"stream":            "/api/v1/stream",
			"stream_node":       "/api/v1/stream/node/:id",
			"stream_service":    "/api/v1/stream/service/:name",
			"stream_allocation": "/api/v1/stream/allocation/:id",
			"logs_cluster":      "/api/v1/logs/cluster",
			"logs_node":         "/api/v1/logs/node/:id",
			"logs_allocation":   "/api/v1/logs/allocation/:id",
			"health":            "/health",
			"metrics":           "/metrics",
		},
		"docs": "https://github.com/yourorg/asty",
	}

	api.writeJSON(w, http.StatusOK, response)
}

// handleHealth returns health status
func (api *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now().Unix(),
	}

	api.writeJSON(w, http.StatusOK, response)
}

// handleStatus returns cluster status
func (api *API) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	nodes, _ := api.server.clusterState.ListNodes()
	leaderInfo, _ := api.server.leaderElection.GetLeader()
	isLeader := api.server.leaderElection.IsLeader()

	healthyNodes := 0
	var leaderNodeID string
	for _, node := range nodes {
		if node.Status == "ready" && time.Since(node.LastSeen) < 2*time.Minute {
			healthyNodes++
		}
		if node.IP == leaderInfo.IP {
			leaderNodeID = node.ID
		}
	}
	if leaderNodeID == "" {
		leaderNodeID = leaderInfo.ID
	}

	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"cluster": map[string]interface{}{
			"leader":        leaderNodeID,
			"leader_ip":     leaderInfo.IP,
			"is_leader":     isLeader,
			"nodes_total":   len(nodes),
			"nodes_healthy": healthyNodes,
		},
		"services": map[string]interface{}{
			"loaded": len(api.server.services),
		},
	})
}

// handleMetrics returns Prometheus metrics
func (api *API) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	nodes, _ := api.server.clusterState.ListNodes()
	healthyNodes := 0
	for _, node := range nodes {
		if node.Status == "ready" && time.Since(node.LastSeen) < 2*time.Minute {
			healthyNodes++
		}
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP asty_nodes_total Total number of nodes\n")
	fmt.Fprintf(w, "# TYPE asty_nodes_total gauge\n")
	fmt.Fprintf(w, "asty_nodes_total %d\n", len(nodes))
	fmt.Fprintf(w, "\n")
	fmt.Fprintf(w, "# HELP asty_nodes_healthy Number of healthy nodes\n")
	fmt.Fprintf(w, "# TYPE asty_nodes_healthy gauge\n")
	fmt.Fprintf(w, "asty_nodes_healthy %d\n", healthyNodes)
	fmt.Fprintf(w, "\n")
	fmt.Fprintf(w, "# HELP asty_services_loaded Number of loaded services\n")
	fmt.Fprintf(w, "# TYPE asty_services_loaded gauge\n")
	fmt.Fprintf(w, "asty_services_loaded %d\n", len(api.server.services))
}
