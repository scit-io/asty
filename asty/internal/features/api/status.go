package api

import (
	"net/http"
	"time"
)

// handleRoot returns API information.
func (api *API) handleRoot(w http.ResponseWriter, r *http.Request) {
	if !methodGuard(w, r, http.MethodGet) {
		return
	}

	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	response := map[string]any{
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

// handleHealth returns health status.
func (api *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !methodGuard(w, r, http.MethodGet) {
		return
	}

	response := map[string]any{
		"status":    "ok",
		"timestamp": time.Now().Unix(),
	}

	api.writeJSON(w, http.StatusOK, response)
}

// handleStatus returns cluster status.
func (api *API) handleStatus(w http.ResponseWriter, r *http.Request) {
	if !methodGuard(w, r, http.MethodGet) {
		return
	}

	nodes, _ := api.ctx.ClusterState().ListNodes()
	leaderInfo, _ := api.ctx.LeaderElection().GetLeader()
	isLeader := api.ctx.LeaderElection().IsLeader()

	healthyNodes := 0
	var leaderNodeID string
	now := time.Now()
	for _, node := range nodes {
		if node.IsHealthy(now) {
			healthyNodes++
		}
		if node.IP == leaderInfo.IP {
			leaderNodeID = node.ID
		}
	}
	if leaderNodeID == "" {
		leaderNodeID = leaderInfo.ID
	}

	api.writeJSON(w, http.StatusOK, map[string]any{
		"cluster": map[string]any{
			"leader":        leaderNodeID,
			"leader_ip":     leaderInfo.IP,
			"is_leader":     isLeader,
			"nodes_total":   len(nodes),
			"nodes_healthy": healthyNodes,
		},
		"services": map[string]any{
			"loaded": len(api.ctx.Services()),
		},
	})
}

// handleMetrics delegates to promhttp.HandlerFor over the private
// Registry initialised in initProm. The Go runtime collector, process
// collector, and asty_* gauges (mirroring the UI's cluster/services
// counters) all live on that registry.
func (api *API) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if !methodGuard(w, r, http.MethodGet) {
		return
	}
	api.promHandler.ServeHTTP(w, r)
}
