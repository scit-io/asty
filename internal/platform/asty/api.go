package asty

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// API provides HTTP API endpoints
type API struct {
	server       *Server
	httpServer   *http.Server
	addr         string
}

// NewAPI creates a new API server
func NewAPI(server *Server, addr string) *API {
	return &API{
		server: server,
		addr:   addr,
	}
}

// Start starts the API server
func (api *API) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Register routes
	mux.HandleFunc("/", api.handleUI)
	mux.HandleFunc("/health", api.handleHealth)
	mux.HandleFunc("/api/v1/nodes", api.handleNodes)
	mux.HandleFunc("/api/v1/services", api.handleServices)
	mux.HandleFunc("/api/v1/allocations", api.handleAllocations)
	mux.HandleFunc("/api/v1/deploy", api.handleDeploy)
	mux.HandleFunc("/api/v1/status", api.handleStatus)
	mux.HandleFunc("/metrics", api.handleMetrics)

	api.httpServer = &http.Server{
		Addr:              api.addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}

	log.Info().Str("addr", api.addr).Msg("API server starting")

	go func() {
		if err := api.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("API server error")
		}
	}()

	<-ctx.Done()

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return api.httpServer.Shutdown(shutdownCtx)
}

// handleUI serves the embedded Web UI
func (api *API) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Only serve UI on root path
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(uiHTML))
}

// handleHealth returns health status
func (api *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := map[string]interface{}{
		"status": "ok",
		"timestamp": time.Now().Unix(),
	}

	api.writeJSON(w, http.StatusOK, response)
}

// handleNodes returns cluster nodes
func (api *API) handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	nodes, err := api.server.clusterState.ListNodes()
	if err != nil {
		api.writeError(w, http.StatusInternalServerError, "failed to list nodes", err)
		return
	}

	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"nodes": nodes,
		"count": len(nodes),
	})
}

// handleServices returns loaded service definitions
func (api *API) handleServices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"services": api.server.services,
		"count":    len(api.server.services),
	})
}

// handleAllocations returns service allocations
func (api *API) handleAllocations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	serviceName := r.URL.Query().Get("service")

	if serviceName != "" {
		// Get allocations for specific service
		allocs, err := api.server.clusterState.ListAllocations(serviceName)
		if err != nil {
			api.writeError(w, http.StatusInternalServerError, "failed to list allocations", err)
			return
		}

		api.writeJSON(w, http.StatusOK, map[string]interface{}{
			"service":     serviceName,
			"allocations": allocs,
			"count":       len(allocs),
		})
		return
	}

	// Get all allocations
	// TODO: implement efficient all-allocations query
	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "use ?service=<name> to get allocations for a specific service",
	})
}

// handleDeploy initiates a deployment
func (api *API) handleDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Service string `json:"service"`
		Version string `json:"version"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}

	if req.Service == "" || req.Version == "" {
		api.writeError(w, http.StatusBadRequest, "service and version required", nil)
		return
	}

	// Check if leader
	if !api.server.leaderElection.IsLeader() {
		leader, _ := api.server.leaderElection.GetLeader()
		api.writeError(w, http.StatusServiceUnavailable, fmt.Sprintf("not leader, current leader: %s", leader), nil)
		return
	}

	// Initiate deployment
	status, err := api.server.DeployService(r.Context(), req.Service, req.Version)
	if err != nil {
		api.writeError(w, http.StatusInternalServerError, "deployment failed", err)
		return
	}

	api.writeJSON(w, http.StatusOK, status)
}

// handleStatus returns cluster status
func (api *API) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	nodes, _ := api.server.clusterState.ListNodes()
	leader, _ := api.server.leaderElection.GetLeader()
	isLeader := api.server.leaderElection.IsLeader()

	healthyNodes := 0
	for _, node := range nodes {
		if node.Status == "ready" && time.Since(node.LastSeen) < 2*time.Minute {
			healthyNodes++
		}
	}

	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"cluster": map[string]interface{}{
			"leader":        leader,
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

	// TODO: implement proper Prometheus metrics
	// For now, return basic text format

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

// writeJSON writes JSON response
func (api *API) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Error().Err(err).Msg("failed to encode JSON response")
	}
}

// writeError writes JSON error response
func (api *API) writeError(w http.ResponseWriter, status int, message string, err error) {
	response := map[string]interface{}{
		"error":   message,
		"status":  status,
	}

	if err != nil {
		response["detail"] = err.Error()
	}

	api.writeJSON(w, status, response)
}
