package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
)

// apiPrefix is the single source of truth for the HTTP namespace under
// which every data route is registered on the orchestrator side.
// Changing this string moves all of the SPA's API calls in one place;
// the SPA reads the matching `API_PREFIX` constant in
// `asty/web/src/api/client.ts` — change both in lockstep.
const apiPrefix = "/metrics"

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

// Start starts the API server. Data routes register on a sub-mux
// with bare paths; the namespace lives in apiPrefix and is added once
// via http.StripPrefix at the outer mux. Methods on the same path
// are separate registrations so the stdlib mux can fan them out.
func (api *API) Start(ctx context.Context) error {
	// Data routes register on a sub-mux with bare paths; the prefix
	// gets added once below via http.StripPrefix(apiPrefix, …) so
	// every route reads naturally and the prefix moves in one place.
	data := http.NewServeMux()
	data.HandleFunc("GET /{$}", api.handleCluster)
	data.HandleFunc("GET /logs", api.handleClusterLogs)

	// Nodes. Writes go through leaderOnly middleware; followers reply
	// 307 to the leader so the operator's client follows transparently.
	data.HandleFunc("GET /nodes", api.handleNodes)
	data.HandleFunc("GET /nodes/{id}", api.handleNode)
	data.HandleFunc("GET /nodes/{id}/drain", api.handleNodeDrainStatus)
	data.HandleFunc("POST /nodes/{id}/drain", api.leaderOnly(api.handleNodeDrain))
	data.HandleFunc("POST /nodes/{id}/pause", api.leaderOnly(api.handleNodePause))
	data.HandleFunc("GET /nodes/{id}/logs", api.handleNodeLogs)
	data.HandleFunc("GET /nodes/{id}/allocations", api.handleNodeAllocations)
	data.HandleFunc("GET /nodes/{id}/allocations/{allocId}", api.handleAllocation)
	data.HandleFunc("GET /nodes/{id}/allocations/{allocId}/logs", api.handleAllocationLogs)
	data.HandleFunc("POST /nodes/{id}/allocations/{allocId}/restart", api.leaderOnly(api.handleAllocationRestart))
	data.HandleFunc("POST /nodes/{id}/allocations/{allocId}/stop", api.leaderOnly(api.handleAllocationStop))

	// Services.
	data.HandleFunc("GET /services", api.handleServices)
	data.HandleFunc("GET /services/{name}", api.handleService)
	data.HandleFunc("POST /services/{name}/scale", api.leaderOnly(api.handleServiceScale))
	data.HandleFunc("GET /services/{name}/allocations", api.handleServiceAllocations)
	data.HandleFunc("GET /services/{name}/autoscaler", api.handleServiceAutoscaler)
	data.HandleFunc("GET /services/{name}/deploy", api.handleServiceDeployHistory)
	data.HandleFunc("POST /services/{name}/deploy", api.leaderOnly(api.handleServiceDeploy))

	// Outer mux: infra endpoints at the root, data namespace nested.
	// /health and /metrics stay at the root because the probes and
	// Prometheus scrape don't know about our prefix.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", api.handleHealth)
	mux.HandleFunc("GET /metrics", api.handleMetrics)
	mux.Handle(apiPrefix+"/", http.StripPrefix(apiPrefix, data))

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
