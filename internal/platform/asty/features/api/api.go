package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// API provides HTTP API endpoints.
type API struct {
	ctx        ServerContext
	httpServer *http.Server
	addr       string
}

// New creates a new API server.
func New(ctx ServerContext, addr string) *API {
	return &API{ctx: ctx, addr: addr}
}

// Start starts the API server.
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
		WriteTimeout:      0, // SSE connections are long-lived
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

// writeJSON writes JSON response.
func (api *API) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Error().Err(err).Msg("failed to encode JSON response")
	}
}

// writeError writes JSON error response.
func (api *API) writeError(w http.ResponseWriter, status int, message string, err error) {
	response := map[string]interface{}{
		"error":  message,
		"status": status,
	}

	if err != nil {
		response["detail"] = err.Error()
	}

	api.writeJSON(w, status, response)
}

// mustJSON marshals v to JSON or returns an empty object on error.
func mustJSON(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		log.Error().Err(err).Msg("api: marshal failed")
		return []byte("{}")
	}
	return b
}
