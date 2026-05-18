package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"asty/asty/internal/api/health"
	"asty/asty/internal/api/prometheus"
	"asty/asty/internal/api/stream"
	"asty/asty/internal/core/types"
	"asty/asty/internal/infra/kv"

	"github.com/rs/zerolog/log"
)

// apiPrefix is the single source of truth for the HTTP namespace under
// which every data route is registered on the orchestrator side.
// Changing this string moves all of the SPA's API calls in one place;
// the SPA reads the matching `API_PREFIX` constant in
// `asty/web/src/api/client.ts` — change both in lockstep.
//
// "/metrics" is the Prometheus exposition path (exact match in the
// outer mux) and no longer overlaps with the data namespace. The
// legacy /metrics/* data alias from migration/tz §14.2 was removed
// after the UI release that ships /api/v1 — see TZ §14.2 sunset note.
const apiPrefix = "/api/v1"

// API provides the orchestrator's dashboard HTTP surface (REST + SSE)
// plus the Prometheus exposition mounted at /metrics. Content
// negotiation per route picks JSON or SSE for the dashboard paths;
// the prometheus handler is built once and reused.
type API struct {
	ctx               ServerContext
	httpServer        *http.Server
	addr              string
	prometheusHandler http.Handler   // built by api/prometheus.Handler.
	streamCtx         stream.Context // narrowed view for api/stream handlers.
}

// New creates a new API server. The prometheus handler is built once
// at construction so the same private registry is reused across
// scrapes (per-instance registry — never the global default — avoids
// the double-register panic during tests). The package-local adapters
// (prometheusAdapter, streamAdapter) narrow ServerContext's concrete
// return types to the smaller interfaces each sub-package declares.
func New(ctx ServerContext, addr string) *API {
	return &API{
		ctx:               ctx,
		addr:              addr,
		prometheusHandler: prometheus.Handler(prometheusAdapter{ctx: ctx}),
		streamCtx:         streamAdapter{ctx: ctx},
	}
}

// prometheusAdapter bridges rest.ServerContext (concrete return types,
// useful elsewhere in api/rest) to prometheus.Context (narrower
// interfaces). Each method body just re-returns the corresponding
// ServerContext call; the implicit interface conversion at the
// return statement does all the type narrowing.
type prometheusAdapter struct{ ctx ServerContext }

func (a prometheusAdapter) ClusterState() *kv.ClusterState       { return a.ctx.ClusterState() }
func (a prometheusAdapter) Services() []*types.ServiceDefinition { return a.ctx.Services() }
func (a prometheusAdapter) StreamHub() prometheus.SnapshotSource { return a.ctx.StreamHub() }
func (a prometheusAdapter) MetricsStore() prometheus.RPSSource   { return a.ctx.MetricsStore() }
func (a prometheusAdapter) Deployer() prometheus.DeployHistorySource {
	return a.ctx.Deployer()
}

// streamAdapter is the equivalent narrowing for api/stream handlers.
// rest.StreamHub is a structural superset of stream.Hub, so the
// implicit interface conversion at the return statement is enough.
type streamAdapter struct{ ctx ServerContext }

func (a streamAdapter) StreamHub() stream.Hub          { return a.ctx.StreamHub() }
func (a streamAdapter) MetricsStore() stream.RPSSource { return a.ctx.MetricsStore() }

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

	// Writes chain tokenAuth → leaderOnly → handler:
	//   tokenAuth refuses without a valid Authorization/X-Asty-Token,
	//   leaderOnly redirects followers to the leader (307),
	//   and only then does the real handler run.
	write := func(h http.HandlerFunc) http.HandlerFunc {
		return api.tokenAuth(api.leaderOnly(h))
	}

	data.HandleFunc("GET /nodes", api.handleNodes)
	data.HandleFunc("GET /nodes/{id}", api.handleNode)
	data.HandleFunc("GET /nodes/{id}/drain", api.handleNodeDrainStatus)
	data.HandleFunc("POST /nodes/{id}/drain", write(api.handleNodeDrain))
	data.HandleFunc("POST /nodes/{id}/pause", write(api.handleNodePause))
	data.HandleFunc("GET /nodes/{id}/logs", api.handleNodeLogs)
	data.HandleFunc("GET /nodes/{id}/allocations", api.handleNodeAllocations)
	data.HandleFunc("GET /nodes/{id}/allocations/{allocId}", api.handleAllocation)
	data.HandleFunc("GET /nodes/{id}/allocations/{allocId}/logs", api.handleAllocationLogs)
	data.HandleFunc("POST /nodes/{id}/allocations/{allocId}/restart", write(api.handleAllocationRestart))
	data.HandleFunc("POST /nodes/{id}/allocations/{allocId}/stop", write(api.handleAllocationStop))

	// Services.
	data.HandleFunc("GET /services", api.handleServices)
	data.HandleFunc("GET /services/{name}", api.handleService)
	data.HandleFunc("POST /services/{name}/scale", write(api.handleServiceScale))
	data.HandleFunc("GET /services/{name}/allocations", api.handleServiceAllocations)
	data.HandleFunc("GET /services/{name}/autoscaler", api.handleServiceAutoscaler)
	data.HandleFunc("GET /services/{name}/deploy", api.handleServiceDeployHistory)
	data.HandleFunc("POST /services/{name}/deploy", write(api.handleServiceDeploy))

	// Outer mux: infra endpoints at the root, data namespace nested.
	// /health and /metrics stay at the root because the probes and
	// Prometheus scrape don't know about our prefix.
	mux := http.NewServeMux()
	mux.Handle("GET /health", health.Handler())
	mux.Handle("GET /metrics", api.prometheusHandler)
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
