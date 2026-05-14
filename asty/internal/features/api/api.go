package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
)

// API provides the orchestrator's HTTP surface: SSE streams, polling
// endpoints, and command POSTs — all on a single port. Content-
// negotiation per route picks the response form (JSON snapshot vs.
// SSE stream vs. Prometheus text) based on the Accept header.
type API struct {
	ctx         ServerContext
	httpServer  *http.Server
	addr        string
	promHandler http.Handler // serves /metrics via promhttp.HandlerFor(privateRegistry).
}

// New creates a new API server.
func New(ctx ServerContext, addr string) *API {
	api := &API{ctx: ctx, addr: addr}
	api.initProm()
	return api
}

// Start starts the API server. Routes are organised by resource path
// (no `/api/v1` prefix); methods on the same path are separate
// registrations so the stdlib mux can fan them out.
func (api *API) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Root: cluster overview.
	mux.HandleFunc("GET /{$}", api.handleCluster)

	// Liveness + Prometheus scrape.
	mux.HandleFunc("GET /health", api.handleHealth)
	mux.HandleFunc("GET /metrics", api.handleMetrics)

	// Cluster events / log stream.
	mux.HandleFunc("GET /logs", api.handleClusterLogs)

	// Nodes.
	mux.HandleFunc("GET /nodes", api.handleNodes)
	mux.HandleFunc("GET /nodes/{id}", api.handleNode)
	mux.HandleFunc("GET /nodes/{id}/drain", api.handleNodeDrainStatus)
	mux.HandleFunc("POST /nodes/{id}/drain", api.handleNodeDrain)
	mux.HandleFunc("POST /nodes/{id}/pause", api.handleNodePause)
	mux.HandleFunc("GET /nodes/{id}/logs", api.handleNodeLogs)
	mux.HandleFunc("GET /nodes/{id}/allocations", api.handleNodeAllocations)
	mux.HandleFunc("GET /nodes/{id}/allocations/{allocId}", api.handleAllocation)
	mux.HandleFunc("GET /nodes/{id}/allocations/{allocId}/logs", api.handleAllocationLogs)
	mux.HandleFunc("POST /nodes/{id}/allocations/{allocId}/restart", api.handleAllocationRestart)
	mux.HandleFunc("POST /nodes/{id}/allocations/{allocId}/stop", api.handleAllocationStop)

	// Services.
	mux.HandleFunc("GET /services", api.handleServices)
	mux.HandleFunc("GET /services/{name}", api.handleService)
	mux.HandleFunc("POST /services/{name}/scale", api.handleServiceScale)
	mux.HandleFunc("GET /services/{name}/allocations", api.handleServiceAllocations)
	mux.HandleFunc("GET /services/{name}/autoscaler", api.handleServiceAutoscaler)
	mux.HandleFunc("GET /services/{name}/deploy", api.handleServiceDeployHistory)
	mux.HandleFunc("POST /services/{name}/deploy", api.handleServiceDeploy)

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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return api.httpServer.Shutdown(shutdownCtx)
}

// writeJSON writes JSON response.
func (api *API) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Error().Err(err).Msg("failed to encode JSON response")
	}
}

// writeError writes JSON error response.
func (api *API) writeError(w http.ResponseWriter, status int, message string, err error) {
	response := map[string]any{
		"error":  message,
		"status": status,
	}

	if err != nil {
		response["detail"] = err.Error()
	}

	api.writeJSON(w, status, response)
}

var mustJSON = types.MustJSON
