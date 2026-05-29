package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"asty/asty/internal/api/health"
	"asty/asty/internal/api/prometheus"
	"asty/asty/internal/api/stream"
	"asty/asty/internal/core/config"
	"asty/asty/internal/core/netutil"
	"asty/asty/internal/core/types"
	"asty/asty/internal/infra/kv"

	"github.com/rs/zerolog/log"
)

// API provides the orchestrator's dashboard HTTP surface (REST + SSE).
// When cfg.Dashboard.Port == cfg.Prometheus.Port the same listener
// also mounts the Prometheus exposition at cfg.Prometheus.Prefix; if
// the ports differ, the prometheus handler is exposed via
// PrometheusHandler() so the composition root can spawn a second
// listener.
type API struct {
	ctx               ServerContext
	httpServer        *http.Server
	cfg               *config.Config
	prometheusHandler http.Handler            // built by api/prometheus.Handler.
	streamCtx         stream.Context          // narrowed view for api/stream handlers.
	corsOrigins       netutil.OriginAllowList // browser Origins allowed to call the surface.
}

// New creates a new API server. The prometheus handler is built once
// at construction so the same private registry is reused across
// scrapes (per-instance registry — never the global default — avoids
// the double-register panic during tests). The package-local adapters
// (prometheusAdapter, streamAdapter) narrow ServerContext's concrete
// return types to the smaller interfaces each sub-package declares.
func New(ctx ServerContext) *API {
	cfg := ctx.Config()
	// A malformed entry falls back to the allow-all set with a loud log
	// rather than deny-all: the dashboard is an admin surface and a
	// silent lockout is worse than a logged typo. Empty config is the
	// normal dev/same-origin case and also yields allow-all.
	origins, err := netutil.ParseOriginAllowList(log.Logger, cfg.Dashboard.AllowedOrigins)
	if err != nil {
		log.Error().Err(err).Msg("dashboard: invalid allowed_origins, falling back to allow-all")
	}
	return &API{
		ctx:               ctx,
		cfg:               cfg,
		prometheusHandler: prometheus.Handler(prometheusAdapter{ctx: ctx}),
		streamCtx:         streamAdapter{ctx: ctx},
		corsOrigins:       origins,
	}
}

// PrometheusHandler returns the Prometheus exposition handler. When
// dashboard and prometheus listeners share a port this handler is
// mounted alongside the dashboard router by Start; when they don't,
// the composition root uses this method to spawn a second listener.
func (api *API) PrometheusHandler() http.Handler { return api.prometheusHandler }

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
// with bare paths; the namespace lives in cfg.Dashboard.Prefix and
// is added once via http.StripPrefix at the outer mux. Methods on
// the same path are separate registrations so the stdlib mux can
// fan them out. When cfg.Dashboard.Port == cfg.Prometheus.Port the
// /metrics handler is mounted on the same listener.
func (api *API) Start(ctx context.Context) error {
	dashboardPrefix := api.cfg.Dashboard.Prefix
	prometheusPrefix := api.cfg.Prometheus.Prefix
	addr := api.cfg.Dashboard.Addr()

	mux := http.NewServeMux()
	mux.Handle("GET /health", health.Handler())
	if api.cfg.Dashboard.Port == api.cfg.Prometheus.Port {
		mux.Handle("GET "+prometheusPrefix, api.prometheusHandler)
	}

	// Writes chain tokenAuth → leaderOnly → auditLog → handler:
	//   tokenAuth refuses without a valid Authorization/X-Asty-Token,
	//   leaderOnly reverse-proxies follower writes to the leader,
	//   auditLog publishes an asty.v1.audit.* event after the
	//     handler returns (status captured from a recorder wrapper),
	//   and only then does the real handler run.
	write := func(h http.HandlerFunc) http.HandlerFunc {
		return api.tokenAuth(api.leaderOnly(api.auditLog(h)))
	}

	// Routes are registered with the full prefix (no StripPrefix) so
	// r.URL.Path stays intact through middleware — the leaderOnly
	// reverse-proxy forwards the exact incoming URL to the leader's
	// mux, which matches on the same prefix.
	route := func(pattern string, h http.HandlerFunc) {
		sp := strings.IndexByte(pattern, ' ')
		mux.HandleFunc(pattern[:sp]+" "+dashboardPrefix+pattern[sp+1:], h)
	}

	route("GET /{$}", api.handleCluster)
	route("GET /logs", api.handleClusterLogs)

	route("GET /nodes", api.handleNodes)
	route("GET /nodes/{id}", api.handleNode)
	route("GET /nodes/{id}/drain", api.handleNodeDrainStatus)
	route("POST /nodes/{id}/drain", write(api.handleNodeDrain))
	route("POST /nodes/{id}/pause", write(api.handleNodePause))
	route("POST /nodes/{id}/kill", write(api.handleNodeKill))
	route("GET /nodes/{id}/logs", api.handleNodeLogs)
	route("GET /nodes/{id}/allocations", api.handleNodeAllocations)
	route("GET /nodes/{id}/allocations/{allocId}", api.handleAllocation)
	route("GET /nodes/{id}/allocations/{allocId}/logs", api.handleAllocationLogs)
	route("POST /nodes/{id}/allocations/{allocId}/restart", write(api.handleAllocationRestart))
	route("POST /nodes/{id}/allocations/{allocId}/stop", write(api.handleAllocationStop))

	route("GET /services", api.handleServices)
	route("GET /services/{name}", api.handleService)
	route("POST /services/{name}/scale", write(api.handleServiceScale))
	route("GET /services/{name}/allocations", api.handleServiceAllocations)
	route("GET /services/{name}/autoscaler", api.handleServiceAutoscaler)
	route("GET /services/{name}/deploy", api.handleServiceDeployHistory)
	route("GET /services/{name}/versions", api.handleServiceVersions)
	route("POST /services/{name}/deploy", write(api.handleServiceDeploy))

	// corsOrigin wraps the whole mux: browser callers get CORS headers,
	// while /metrics and /health (no Origin) pass through untouched.
	api.httpServer = &http.Server{
		Addr:              addr,
		Handler:           api.corsOrigin(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      0, // SSE connections are long-lived
	}

	log.Info().Str("addr", addr).Str("prefix", dashboardPrefix).Msg("dashboard API listening")

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
