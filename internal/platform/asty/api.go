package asty

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/nats-io/nats.go"
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
	mux.HandleFunc("/", api.handleRoot)
	mux.HandleFunc("/health", api.handleHealth)
	mux.HandleFunc("/api/v1/nodes/", api.handleNodesWithID)
	mux.HandleFunc("/api/v1/nodes", api.handleNodes)
	mux.HandleFunc("/api/v1/services/", api.handleServicesWithActions)
	mux.HandleFunc("/api/v1/services", api.handleServices)
	mux.HandleFunc("/api/v1/allocations/", api.handleAllocationWithID)
	mux.HandleFunc("/api/v1/allocations", api.handleAllocations)
	mux.HandleFunc("/api/v1/deploy", api.handleDeploy)
	mux.HandleFunc("/api/v1/deployments", api.handleDeployments)
	mux.HandleFunc("/api/v1/status", api.handleStatus)
	mux.HandleFunc("/api/v1/events", api.handleEvents)
	mux.HandleFunc("/api/v1/stream/node/", api.handleStreamNode)
	mux.HandleFunc("/api/v1/stream/service/", api.handleStreamService)
	mux.HandleFunc("/api/v1/stream/allocation/", api.handleStreamAllocation)
	mux.HandleFunc("/api/v1/stream/metrics/cluster", api.handleStreamMetricsCluster)
	mux.HandleFunc("/api/v1/stream", api.handleStream)
	mux.HandleFunc("/api/v1/autoscaler/events", api.handleAutoscalerEvents)
	mux.HandleFunc("/api/v1/autoscaler/status", api.handleAutoscalerStatus)
	mux.HandleFunc("/api/v1/logs/cluster", api.handleLogsCluster)
	mux.HandleFunc("/api/v1/logs/allocation/", api.handleLogsAllocation)
	mux.HandleFunc("/api/v1/logs/node/", api.handleLogsNode)
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

// handleRoot returns API information
func (api *API) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Only serve on root path
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	response := map[string]interface{}{
		"service": "Asty Orchestrator",
		"version": "0.1.0",
		"api":     "/api/v1",
		"endpoints": map[string]string{
			"status":             "/api/v1/status",
			"nodes":              "/api/v1/nodes",
			"services":           "/api/v1/services",
			"allocations":        "/api/v1/allocations",
			"deploy":             "/api/v1/deploy",
			"deployments":        "/api/v1/deployments",
			"autoscaler_status":  "/api/v1/autoscaler/status",
			"autoscaler_events":  "/api/v1/autoscaler/events",
			"stream":             "/api/v1/stream",
			"stream_node":        "/api/v1/stream/node/:id",
			"stream_service":     "/api/v1/stream/service/:name",
			"stream_allocation":  "/api/v1/stream/allocation/:id",
			"stream_metrics":     "/api/v1/stream/metrics/cluster",
			"logs_cluster":       "/api/v1/logs/cluster",
			"logs_node":          "/api/v1/logs/node/:id",
			"logs_allocation":    "/api/v1/logs/allocation/:id",
			"health":             "/health",
			"metrics":            "/metrics",
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

	// Enrich nodes with allocation counts
	for _, node := range nodes {
		running := 0
		planned := 0

		// Iterate through all services to count allocations for this node
		for _, svc := range api.server.services {
			allocs, err := api.server.clusterState.ListAllocations(svc.Name)
			if err != nil {
				continue
			}
			for _, alloc := range allocs {
				if alloc.NodeID == node.ID {
					planned++
					if alloc.Status == "running" {
						running++
					}
				}
			}
		}

		node.AllocationsRunning = running
		node.AllocationsPlanned = planned
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
	nodeID := r.URL.Query().Get("node_id")

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

	if nodeID != "" {
		// Get allocations for specific node - iterate through all services
		var allAllocs []*ServiceAllocation
		for _, svc := range api.server.services {
			allocs, err := api.server.clusterState.ListAllocations(svc.Name)
			if err != nil {
				continue
			}
			for _, alloc := range allocs {
				if alloc.NodeID == nodeID {
					allAllocs = append(allAllocs, alloc)
				}
			}
		}

		api.writeJSON(w, http.StatusOK, map[string]interface{}{
			"node_id":     nodeID,
			"allocations": allAllocs,
			"count":       len(allAllocs),
		})
		return
	}

	// Get all allocations
	// TODO: implement efficient all-allocations query
	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "use ?service=<name> or ?node_id=<id> to get allocations",
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
		leaderInfo, _ := api.server.leaderElection.GetLeader()
		api.writeError(w, http.StatusServiceUnavailable, fmt.Sprintf("not leader, current leader: %s", leaderInfo.ID), nil)
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

// handleNodesWithID handles /api/v1/nodes/:id and /api/v1/nodes/:id/action
func (api *API) handleNodesWithID(w http.ResponseWriter, r *http.Request) {
	// Extract node ID and optional action from path
	path := r.URL.Path[len("/api/v1/nodes/"):]
	if path == "" {
		api.handleNodes(w, r)
		return
	}

	var nodeID, action string
	for i, ch := range path {
		if ch == '/' {
			nodeID = path[:i]
			action = path[i+1:]
			break
		}
	}
	if nodeID == "" {
		nodeID = path
	}

	// Handle actions
	if action != "" {
		switch action {
		case "drain":
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			api.handleNodeDrain(w, r, nodeID)
			return
		case "drain/status":
			if r.Method != http.MethodGet {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			api.handleNodeDrainStatus(w, r, nodeID)
			return
		case "pause":
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			// TODO: implement actual pause logic
			api.writeJSON(w, http.StatusOK, map[string]interface{}{
				"node_id": nodeID,
				"action":  "pause",
				"message": "pause initiated (not yet fully implemented)",
			})
			return
		default:
			api.writeError(w, http.StatusBadRequest, "unknown action", nil)
			return
		}
	}

	// Handle GET /api/v1/nodes/:id (node details)
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	node, err := api.server.clusterState.GetNode(nodeID)
	if err != nil {
		api.writeError(w, http.StatusNotFound, "node not found", err)
		return
	}

	// Enrich node with allocation counts
	running := 0
	planned := 0

	for _, svc := range api.server.services {
		allocs, err := api.server.clusterState.ListAllocations(svc.Name)
		if err != nil {
			continue
		}
		for _, alloc := range allocs {
			if alloc.NodeID == node.ID {
				planned++
				if alloc.Status == "running" {
					running++
				}
			}
		}
	}

	node.AllocationsRunning = running
	node.AllocationsPlanned = planned

	api.writeJSON(w, http.StatusOK, node)
}

// handleServicesWithActions handles /api/v1/services/:name and /api/v1/services/:name/action
func (api *API) handleServicesWithActions(w http.ResponseWriter, r *http.Request) {
	// Extract service name and optional action from path
	path := r.URL.Path[len("/api/v1/services/"):]
	if path == "" {
		api.handleServices(w, r)
		return
	}

	var serviceName, action string
	for i, ch := range path {
		if ch == '/' {
			serviceName = path[:i]
			action = path[i+1:]
			break
		}
	}
	if serviceName == "" {
		serviceName = path
	}

	// Handle actions (POST only)
	if action != "" {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if action == "scale" {
			var req struct {
				Count int `json:"count"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				api.writeError(w, http.StatusBadRequest, "invalid request body", err)
				return
			}

			// TODO: implement actual scaling logic
			api.writeJSON(w, http.StatusOK, map[string]interface{}{
				"service": serviceName,
				"count":   req.Count,
				"message": "scaling not yet implemented",
			})
			return
		}

		api.writeError(w, http.StatusNotFound, "unknown action", nil)
		return
	}

	// Handle GET /api/v1/services/:name (service details)
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Find service definition
	var service *ServiceDefinition
	for _, svc := range api.server.services {
		if svc.Name == serviceName {
			service = svc
			break
		}
	}

	if service == nil {
		api.writeError(w, http.StatusNotFound, "service not found", nil)
		return
	}

	// Get allocations for this service
	allocs, _ := api.server.clusterState.ListAllocations(serviceName)

	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"service":     service,
		"allocations": allocs,
	})
}

// handleEvents returns cluster events
func (api *API) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// TODO: implement events storage
	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"events": []interface{}{},
		"count":  0,
	})
}

// handleAllocationWithID handles /api/v1/allocations/:id and /api/v1/allocations/:id/action
func (api *API) handleAllocationWithID(w http.ResponseWriter, r *http.Request) {
	// Extract allocation ID and optional action: /api/v1/allocations/:id or /api/v1/allocations/:id/restart
	path := r.URL.Path[len("/api/v1/allocations/"):]
	if path == "" {
		api.handleAllocations(w, r)
		return
	}

	var allocID, action string
	for i, ch := range path {
		if ch == '/' {
			allocID = path[:i]
			action = path[i+1:]
			break
		}
	}
	if allocID == "" {
		allocID = path
	}

	// Handle actions (POST only)
	if action != "" {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		switch action {
		case "restart":
			// TODO: implement restart logic - send command to agent via NATS
			api.writeJSON(w, http.StatusOK, map[string]interface{}{
				"allocation_id": allocID,
				"action":        "restart",
				"message":       "restart initiated (not yet fully implemented)",
			})
			return
		case "stop":
			// TODO: implement stop logic - send command to agent via NATS
			api.writeJSON(w, http.StatusOK, map[string]interface{}{
				"allocation_id": allocID,
				"action":        "stop",
				"message":       "stop initiated (not yet fully implemented)",
			})
			return
		default:
			api.writeError(w, http.StatusBadRequest, "unknown action", nil)
			return
		}
	}

	// Handle GET /api/v1/allocations/:id (allocation details)
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Find allocation across all services
	// TODO: optimize this - maybe store allocations by ID in a separate index
	services := api.server.services
	for _, svc := range services {
		allocs, err := api.server.clusterState.ListAllocations(svc.Name)
		if err != nil {
			continue
		}
		for _, alloc := range allocs {
			if alloc.ID == allocID {
				api.writeJSON(w, http.StatusOK, alloc)
				return
			}
		}
	}

	api.writeError(w, http.StatusNotFound, "allocation not found", nil)
}

// handleDeployments returns deployment history
func (api *API) handleDeployments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	history := api.server.deployer.GetHistory()

	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"deployments": history,
		"count":       len(history),
	})
}

// handleAutoscalerEvents returns autoscaler scaling events
func (api *API) handleAutoscalerEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	service := r.URL.Query().Get("service")
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}

	events := api.server.metricsStore.GetEvents(service, limit)

	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"events": events,
		"count":  len(events),
	})
}

// handleAutoscalerStatus returns current autoscaler state per service
func (api *API) handleAutoscalerStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	servicesStatus := make(map[string]interface{})

	for _, svc := range api.server.services {
		allocs, _ := api.server.clusterState.ListAllocations(svc.Name)
		running := 0
		for _, a := range allocs {
			if a.Status == "running" {
				running++
			}
		}

		cooldownUp := false
		cooldownDown := false
		var lastAction string
		var lastActionAt int64

		if cd, err := api.server.clusterState.GetServiceCooldown(svc.Name); err == nil {
			if !cd.LastScaleUp.IsZero() {
				if time.Since(cd.LastScaleUp) < api.server.cfg.CooldownUp {
					cooldownUp = true
				}
				lastAction = "scale_up"
				lastActionAt = cd.LastScaleUp.Unix()
			}
			if !cd.LastScaleDown.IsZero() {
				if time.Since(cd.LastScaleDown) < api.server.cfg.CooldownDown {
					cooldownDown = true
				}
				if cd.LastScaleDown.Unix() > lastActionAt {
					lastAction = "scale_down"
					lastActionAt = cd.LastScaleDown.Unix()
				}
			}
		}

		servicesStatus[svc.Name] = map[string]interface{}{
			"current_copies":     running,
			"min_copies":         api.server.cfg.MinCopies,
			"target_cpu":         api.server.cfg.TargetCPU,
			"target_memory":      api.server.cfg.TargetMemory,
			"traffic_threshold":  api.server.cfg.TrafficRPSThreshold,
			"cooldown_up_active": cooldownUp,
			"cooldown_down_active": cooldownDown,
			"last_action":        lastAction,
			"last_action_at":     lastActionAt,
		}
	}

	api.writeJSON(w, http.StatusOK, map[string]interface{}{
		"services": servicesStatus,
	})
}

// sseSetup writes SSE headers and returns a flusher. On unsupported writers it
// sends an error response and returns nil.
func sseSetup(w http.ResponseWriter) http.Flusher {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return nil
	}
	return flusher
}

// sseEvent writes a single SSE event (event: name, data: json, blank line).
func sseEvent(w http.ResponseWriter, event string, data []byte) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
}

// handleStream handles the global SSE stream — cluster status, nodes, services
// (with usage), and drain progress. Reads from streamHub.
func (api *API) handleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher := sseSetup(w)
	if flusher == nil {
		return
	}

	hub := api.server.streamHub
	snapshots, unsubSnap := hub.Subscribe()
	defer unsubSnap()
	drainCh, unsubDrain := hub.SubscribeDrain()
	defer unsubDrain()

	emit := func(snap *clusterSnapshot) {
		sseEvent(w, "status", mustJSON(map[string]interface{}{
			"cluster":   snap.Cluster,
			"services":  map[string]interface{}{"loaded": len(snap.Services)},
			"timestamp": snap.Timestamp,
		}))
		sseEvent(w, "nodes", mustJSON(map[string]interface{}{"nodes": snap.Nodes}))
		sseEvent(w, "services", mustJSON(map[string]interface{}{"services": snap.Services}))
		flusher.Flush()
	}

	// Keepalive ping: matters when proxies have idle timeouts (nginx default 60s).
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case snap, ok := <-snapshots:
			if !ok {
				return
			}
			emit(snap)
		case data, ok := <-drainCh:
			if !ok {
				return
			}
			sseEvent(w, "drain_progress", data)
			flusher.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// handleStreamNode streams allocations + metrics for a single node.
func (api *API) handleStreamNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	nodeID := r.URL.Path[len("/api/v1/stream/node/"):]
	if nodeID == "" {
		http.Error(w, "node ID required", http.StatusBadRequest)
		return
	}

	flusher := sseSetup(w)
	if flusher == nil {
		return
	}

	snapshots, unsub := api.server.streamHub.Subscribe()
	defer unsub()

	emit := func(snap *clusterSnapshot) {
		allocs := snap.AllocsByNode[nodeID]
		if allocs == nil {
			allocs = []*ServiceAllocation{}
		}
		sseEvent(w, "allocations", mustJSON(map[string]interface{}{"allocations": allocs}))

		since := time.Now().Add(-1 * time.Hour)
		ms := api.server.metricsStore
		sseEvent(w, "metrics", mustJSON(map[string]interface{}{
			"cpu":    ms.Get("node."+nodeID+".cpu", since),
			"memory": ms.Get("node."+nodeID+".memory", since),
			"rps":    ms.Get("node."+nodeID+".rps", since),
		}))
		flusher.Flush()
	}

	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case snap, ok := <-snapshots:
			if !ok {
				return
			}
			emit(snap)
		case <-ping.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// handleStreamService streams definition + allocations + metrics for a service.
func (api *API) handleStreamService(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	serviceName := r.URL.Path[len("/api/v1/stream/service/"):]
	if serviceName == "" {
		http.Error(w, "service name required", http.StatusBadRequest)
		return
	}

	flusher := sseSetup(w)
	if flusher == nil {
		return
	}

	snapshots, unsub := api.server.streamHub.Subscribe()
	defer unsub()

	emit := func(snap *clusterSnapshot) {
		var svcDef *ServiceDefinition
		for _, svc := range snap.Services {
			if svc.Name == serviceName {
				svcDef = svc.ServiceDefinition
				break
			}
		}
		allocs := snap.AllocsByService[serviceName]
		if allocs == nil {
			allocs = []*ServiceAllocation{}
		}

		sseEvent(w, "detail", mustJSON(map[string]interface{}{
			"service":     svcDef,
			"allocations": allocs,
		}))

		since := time.Now().Add(-1 * time.Hour)
		ms := api.server.metricsStore
		sseEvent(w, "metrics", mustJSON(map[string]interface{}{
			"cpu":               ms.Get("service."+serviceName+".cpu", since),
			"memory":            ms.Get("service."+serviceName+".memory", since),
			"allocations_count": ms.Get("service."+serviceName+".alloc_count", since),
		}))
		flusher.Flush()
	}

	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case snap, ok := <-snapshots:
			if !ok {
				return
			}
			emit(snap)
		case <-ping.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// handleStreamAllocation streams a single allocation's detail + metrics.
func (api *API) handleStreamAllocation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	allocID := r.URL.Path[len("/api/v1/stream/allocation/"):]
	if allocID == "" {
		http.Error(w, "allocation ID required", http.StatusBadRequest)
		return
	}

	flusher := sseSetup(w)
	if flusher == nil {
		return
	}

	snapshots, unsub := api.server.streamHub.Subscribe()
	defer unsub()

	emit := func(snap *clusterSnapshot) {
		alloc := snap.AllocByID[allocID]
		sseEvent(w, "detail", mustJSON(map[string]interface{}{"allocation": alloc}))

		since := time.Now().Add(-1 * time.Hour)
		ms := api.server.metricsStore
		sseEvent(w, "metrics", mustJSON(map[string]interface{}{
			"cpu":    ms.Get("alloc."+allocID+".cpu", since),
			"memory": ms.Get("alloc."+allocID+".memory", since),
		}))
		flusher.Flush()
	}

	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case snap, ok := <-snapshots:
			if !ok {
				return
			}
			emit(snap)
		case <-ping.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// handleStreamMetricsCluster streams cluster-level metric time-series.
func (api *API) handleStreamMetricsCluster(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher := sseSetup(w)
	if flusher == nil {
		return
	}

	snapshots, unsub := api.server.streamHub.Subscribe()
	defer unsub()

	emit := func() {
		since := time.Now().Add(-1 * time.Hour)
		ms := api.server.metricsStore
		sseEvent(w, "metrics", mustJSON(map[string]interface{}{
			"cpu":    ms.Get("cluster.cpu", since),
			"memory": ms.Get("cluster.memory", since),
			"rps":    ms.Get("cluster.rps", since),
		}))
		flusher.Flush()
	}

	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-snapshots:
			if !ok {
				return
			}
			emit()
		case <-ping.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
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

// handleLogsAllocation returns logs for an allocation via SSE
func (api *API) handleLogsAllocation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract allocation ID from path: /api/v1/logs/allocation/:id
	allocID := r.URL.Path[len("/api/v1/logs/allocation/"):]
	if allocID == "" {
		api.writeError(w, http.StatusBadRequest, "allocation ID required", nil)
		return
	}

	// Parse query parameters
	lines := 100 // default
	if l := r.URL.Query().Get("lines"); l != "" {
		fmt.Sscanf(l, "%d", &lines)
	}

	follow := r.URL.Query().Get("follow") == "true"

	// Find allocation to get node_id and service_name
	var allocation *ServiceAllocation
	for _, svc := range api.server.services {
		allocs, err := api.server.clusterState.ListAllocations(svc.Name)
		if err != nil {
			continue
		}
		for _, alloc := range allocs {
			if alloc.ID == allocID {
				allocation = alloc
				break
			}
		}
		if allocation != nil {
			break
		}
	}

	if allocation == nil {
		api.writeError(w, http.StatusNotFound, "allocation not found", nil)
		return
	}

	// Request initial logs from agent via NATS
	cmdData, err := MarshalGetLogsCommand(allocation.ServiceName, lines, false)
	if err != nil {
		api.writeError(w, http.StatusInternalServerError, "failed to create logs command", err)
		return
	}

	subject := fmt.Sprintf("asty.v1.agent.%s.cmd", allocation.NodeID)

	msg, err := api.server.nc.Request(subject, cmdData, 5*time.Second)
	if err != nil {
		log.Error().Err(err).Str("node_id", allocation.NodeID).Msg("failed to request logs from agent")
		api.writeError(w, http.StatusServiceUnavailable, "failed to retrieve logs from agent", err)
		return
	}

	// Parse logs response
	var logsResp LogsResponse
	if err := json.Unmarshal(msg.Data, &logsResp); err != nil {
		api.writeError(w, http.StatusInternalServerError, "failed to parse logs response", err)
		return
	}

	if !logsResp.Success {
		api.writeError(w, http.StatusInternalServerError, "agent failed to retrieve logs", fmt.Errorf("%s", logsResp.Error))
		return
	}

	// If not following, return JSON with initial logs
	if !follow {
		api.writeJSON(w, http.StatusOK, map[string]interface{}{
			"allocation_id": allocID,
			"service_name":  allocation.ServiceName,
			"node_id":       allocation.NodeID,
			"logs":          logsResp.Logs,
			"line_count":    len(logsResp.Logs),
		})
		return
	}

	// SSE streaming mode
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Send initial logs
	for _, line := range logsResp.Logs {
		logEntry, _ := json.Marshal(map[string]interface{}{
			"line": line,
			"timestamp": time.Now().Unix(),
		})
		fmt.Fprintf(w, "data: %s\n\n", logEntry)
	}
	flusher.Flush()

	// Subscribe to log stream via NATS
	streamSubject := fmt.Sprintf("asty.v1.agent.%s.logs.%s", allocation.NodeID, allocation.ServiceName)
	sub, err := api.server.nc.Subscribe(streamSubject, func(msg *nats.Msg) {
		// Forward log line to SSE
		fmt.Fprintf(w, "data: %s\n\n", msg.Data)
		flusher.Flush()
	})

	if err != nil {
		log.Error().Err(err).Str("subject", streamSubject).Msg("failed to subscribe to log stream")
		return
	}
	defer sub.Unsubscribe()

	log.Info().
		Str("allocation_id", allocID).
		Str("subject", streamSubject).
		Msg("streaming logs via SSE")

	// Keep connection alive until client disconnects
	<-r.Context().Done()

	log.Info().Str("allocation_id", allocID).Msg("log stream closed")
}


// handleLogsNode returns logs for a node (agent logs)
func (api *API) handleLogsNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract node ID from path: /api/v1/logs/node/:id
	nodeID := r.URL.Path[len("/api/v1/logs/node/"):]
	if nodeID == "" {
		api.writeError(w, http.StatusBadRequest, "node ID required", nil)
		return
	}

	// Check if node exists
	_, err := api.server.clusterState.GetNode(nodeID)
	if err != nil {
		api.writeError(w, http.StatusNotFound, "node not found", err)
		return
	}

	// Parse query parameters
	lines := 100 // default
	if l := r.URL.Query().Get("lines"); l != "" {
		fmt.Sscanf(l, "%d", &lines)
	}

	follow := r.URL.Query().Get("follow") == "true"

	// Non-streaming mode: return placeholder
	if !follow {
		api.writeJSON(w, http.StatusOK, map[string]interface{}{
			"node_id": nodeID,
			"logs": []string{
				fmt.Sprintf("[%s] [info] Node agent log stream available via SSE (follow=true)", time.Now().Format(time.RFC3339)),
				"[asty] Use follow=true for real-time agent events",
			},
			"line_count": 2,
		})
		return
	}

	// SSE streaming mode
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Subscribe to node log stream via NATS
	streamSubject := fmt.Sprintf("asty.v1.agent.%s.logs.agent", nodeID)

	sub, err := api.server.nc.Subscribe(streamSubject, func(msg *nats.Msg) {
		// msg.Data contains JSON: {"timestamp": ..., "level": ..., "message": ..., ...}
		var entry map[string]interface{}
		if err := json.Unmarshal(msg.Data, &entry); err != nil {
			return
		}

		// Format log line for UI
		level := entry["level"]
		message := entry["message"]

		var timeStr string
		if t, ok := entry["time"].(string); ok {
			timeStr = t
		} else if ts, ok := entry["timestamp"].(float64); ok {
			timeStr = time.Unix(int64(ts), 0).Format(time.RFC3339)
		} else {
			timeStr = time.Now().Format(time.RFC3339)
		}

		logLine := fmt.Sprintf("[%s] [%s] %s", timeStr, level, message)

		// Add extra fields if present
		delete(entry, "timestamp")
		delete(entry, "time")
		delete(entry, "level")
		delete(entry, "message")

		if len(entry) > 0 {
			extraJSON, _ := json.Marshal(entry)
			logLine += " " + string(extraJSON)
		}

		// Send to SSE
		logEntry, _ := json.Marshal(map[string]interface{}{
			"line":      logLine,
			"timestamp": entry["timestamp"],
		})
		fmt.Fprintf(w, "data: %s\n\n", logEntry)
		flusher.Flush()
	})

	if err != nil {
		log.Error().Err(err).Str("subject", streamSubject).Msg("failed to subscribe to node log stream")
		http.Error(w, "Failed to subscribe to log stream", http.StatusInternalServerError)
		return
	}
	defer sub.Unsubscribe()

	// Send initial message
	initMsg, _ := json.Marshal(map[string]interface{}{
		"line":      fmt.Sprintf("[%s] [info] Node agent log stream connected", time.Now().Format(time.RFC3339)),
		"timestamp": time.Now().Unix(),
	})
	fmt.Fprintf(w, "data: %s\n\n", initMsg)
	flusher.Flush()

	log.Info().Str("node_id", nodeID).Str("subject", streamSubject).Msg("node log stream opened")

	// Keep connection alive until client disconnects
	<-r.Context().Done()

	log.Info().Str("node_id", nodeID).Msg("node log stream closed")
}

// handleLogsCluster returns cluster-wide logs (server logs) via SSE
func (api *API) handleLogsCluster(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse query parameters
	lines := 100 // default
	if l := r.URL.Query().Get("lines"); l != "" {
		fmt.Sscanf(l, "%d", &lines)
	}

	follow := r.URL.Query().Get("follow") == "true"

	// Non-streaming mode: return recent logs (not implemented yet - would need log buffer)
	if !follow {
		// Return JSON with placeholder - real implementation would need ring buffer
		api.writeJSON(w, http.StatusOK, map[string]interface{}{
			"logs": []string{
				fmt.Sprintf("[%s] [info] Cluster log stream available via SSE (follow=true)", time.Now().Format(time.RFC3339)),
				"[asty] Use follow=true for real-time cluster events",
			},
			"line_count": 2,
		})
		return
	}

	// SSE streaming mode
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Subscribe to cluster log stream via NATS
	streamSubject := "asty.v1.server.logs"

	sub, err := api.server.nc.Subscribe(streamSubject, func(msg *nats.Msg) {
		// msg.Data already contains JSON: {"timestamp": ..., "level": ..., "message": ..., ...}
		// Parse it to extract line and format for UI
		var entry map[string]interface{}
		if err := json.Unmarshal(msg.Data, &entry); err != nil {
			return
		}

		// Format log line for UI
		level := entry["level"]
		message := entry["message"]

		var timeStr string
		if t, ok := entry["time"].(string); ok {
			timeStr = t
		} else if ts, ok := entry["timestamp"].(float64); ok {
			timeStr = time.Unix(int64(ts), 0).Format(time.RFC3339)
		} else {
			timeStr = time.Now().Format(time.RFC3339)
		}

		logLine := fmt.Sprintf("[%s] [%s] %s", timeStr, level, message)

		// Add extra fields if present
		delete(entry, "timestamp")
		delete(entry, "time")
		delete(entry, "level")
		delete(entry, "message")

		if len(entry) > 0 {
			extraJSON, _ := json.Marshal(entry)
			logLine += " " + string(extraJSON)
		}

		// Send to SSE
		logEntry, _ := json.Marshal(map[string]interface{}{
			"line":      logLine,
			"timestamp": entry["timestamp"],
		})
		fmt.Fprintf(w, "data: %s\n\n", logEntry)
		flusher.Flush()
	})

	if err != nil {
		log.Error().Err(err).Str("subject", streamSubject).Msg("failed to subscribe to cluster log stream")
		http.Error(w, "Failed to subscribe to log stream", http.StatusInternalServerError)
		return
	}
	defer sub.Unsubscribe()

	// Send initial message
	initMsg, _ := json.Marshal(map[string]interface{}{
		"line":      fmt.Sprintf("[%s] [info] Cluster log stream connected", time.Now().Format(time.RFC3339)),
		"timestamp": time.Now().Unix(),
	})
	fmt.Fprintf(w, "data: %s\n\n", initMsg)
	flusher.Flush()

	log.Info().Str("subject", streamSubject).Msg("cluster log stream opened")

	// Keep connection alive until client disconnects
	<-r.Context().Done()

	log.Info().Msg("cluster log stream closed")
}

// handleNodeDrain handles POST /api/v1/nodes/:id/drain
// Request body: {"enable": true} to start drain, {"enable": false} to resume
func (api *API) handleNodeDrain(w http.ResponseWriter, r *http.Request, nodeID string) {
	var req struct {
		Enable bool `json:"enable"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Default to enable=true if no body
		req.Enable = true
	}

	if !req.Enable {
		if err := api.server.drainManager.Resume(nodeID); err != nil {
			api.writeError(w, http.StatusBadRequest, "resume failed", err)
			return
		}
		api.writeJSON(w, http.StatusOK, map[string]interface{}{
			"node_id": nodeID,
			"status":  "ready",
			"message": "drain cancelled, node resumed",
		})
		return
	}

	status, err := api.server.drainManager.Start(nodeID)
	if err != nil {
		code := http.StatusBadRequest
		if status != nil {
			// Already draining — return current status
			api.writeJSON(w, http.StatusOK, status)
			return
		}
		api.writeError(w, code, "drain failed", err)
		return
	}

	api.writeJSON(w, http.StatusOK, status)
}

// handleNodeDrainStatus handles GET /api/v1/nodes/:id/drain/status
func (api *API) handleNodeDrainStatus(w http.ResponseWriter, _ *http.Request, nodeID string) {
	status := api.server.drainManager.GetStatus(nodeID)
	if status == nil {
		// Check if node is drained but operation already completed
		node, err := api.server.clusterState.GetNode(nodeID)
		if err != nil {
			api.writeError(w, http.StatusNotFound, "node not found", err)
			return
		}
		api.writeJSON(w, http.StatusOK, &DrainStatus{
			NodeID:           nodeID,
			Status:           node.Status,
			TotalAllocations: 0,
			Migrated:         0,
			Remaining:        0,
			Errors:           []string{},
		})
		return
	}
	api.writeJSON(w, http.StatusOK, status)
}

