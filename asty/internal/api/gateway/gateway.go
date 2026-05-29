// Package gateway is the HTTP/WebSocket entry point embedded in the
// asty agent process. It proxies HTTP requests and upgrades WebSocket
// connections to NATS, using the agent's own NATS connection — no
// extra process, no extra connection.
package gateway

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"asty/asty/internal/core/config"
	"asty/asty/internal/core/netutil"
	"asty/asty/internal/core/types"

	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
)

// WebSocket protocol constants. Keep them in this file alongside Gateway
// — they are inherent to the WS bridge, not configurable per deployment.
const (
	// wsWriteDeadline bounds a single write so a slow/dead client cannot
	// stall the writer goroutine.
	wsWriteDeadline = 10 * time.Second

	// wsReadDeadline is the maximum gap between client frames or Pong.
	// On expiry the gateway treats the session as dead.
	wsReadDeadline = 60 * time.Second

	// wsPingInterval is how often the gateway pings the client. Must be
	// less than wsReadDeadline so Pong returns in time.
	wsPingInterval = 30 * time.Second

	// wsReadLimit caps a single inbound WS frame. Without it gorilla
	// reads frames of any size — memory-DoS in waiting.
	wsReadLimit = 64 * 1024
)

// Gateway is the in-process HTTP/WebSocket gateway. One instance per
// agent; lifecycle bound to the agent's context.
type Gateway struct {
	nats         *nats.Conn
	cfg          config.GatewayConfig
	nodeID       string
	upgrader     websocket.Upgrader
	allowedHosts netutil.OriginAllowList
	rl           *rateLimiter
	log          zerolog.Logger

	// validRequests counts /v1/* requests that survived the Origin and
	// rate-limit middlewares — what the autoscaler treats as real
	// traffic on this node. Sampled by reportRPSLoop.
	validRequests atomic.Int64

	// serviceRequests sharded by the first path segment (service name).
	// Bumped from the same point as validRequests so the two stay in
	// agreement; sampled by ReportRPSLoop to attribute traffic to
	// individual allocations on this node.
	serviceRequests sync.Map // map[string]*atomic.Int64

	// ctx is the gateway-scoped context (the agent's). WS handlers and
	// HTTP requests observe shutdown when the agent cancels it.
	ctx context.Context
}

// bumpService increments the per-service surviving-request counter for
// the path segment route() parsed out. Lazy-init via LoadOrStore so the
// fast path (counter already present) avoids allocating.
func (gw *Gateway) bumpService(service string) {
	v, ok := gw.serviceRequests.Load(service)
	if !ok {
		v, _ = gw.serviceRequests.LoadOrStore(service, new(atomic.Int64))
	}
	v.(*atomic.Int64).Add(1)
}

// New builds a gateway bound to nc and cfg. nodeID is the agent's
// own node id, used as the suffix of the RPS report subject so the
// server can attribute traffic per node. serviceRules are rate-limit
// rules collected from all loaded .asty service definitions — the
// gateway enforces them on incoming requests before proxying to NATS.
func New(ctx context.Context, nc *nats.Conn, cfg config.GatewayConfig, nodeID string, serviceRules []types.RateLimitRule, log zerolog.Logger) (*Gateway, error) {
	hosts, err := netutil.ParseOriginAllowList(log, cfg.AllowedHosts)
	if err != nil {
		return nil, err
	}

	gw := &Gateway{
		nats:         nc,
		cfg:          cfg,
		nodeID:       nodeID,
		allowedHosts: hosts,
		rl:           newRateLimiter(cfg.RateLimit, serviceRules, log, ctx.Done()),
		log:          log,
		ctx:          ctx,
	}
	gw.upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return hosts.Allows(log, r.Header.Get("Origin"))
		},
	}
	return gw, nil
}

// RootContext returns a context that tracks gateway shutdown. Wire it
// into http.Server.BaseContext so each request's r.Context() cancels
// on shutdown, letting NATS round-trips abort promptly.
func (gw *Gateway) RootContext() context.Context { return gw.ctx }

// Handler returns the root http.Handler of the gateway.
//
// Routes:
//   - /health             — health check (no rate limit, no Origin check, no NATS proxy)
//   - cfg.Prefix + "/"    — API: Origin → RateLimit → HTTP RPC or WebSocket
//
// cfg.Prefix defaults to /api/v1 and is configurable via
// A_GATEWAY_PREFIX. The router strips the prefix before handing the
// trimmed path to gw.route, which preserves the existing path-validation
// rules (only segments AFTER the prefix become NATS subject tokens).
func (gw *Gateway) Handler() http.Handler {
	prefix := gw.cfg.Prefix
	if prefix == "" {
		prefix = "/api/v1"
	}

	api := http.NewServeMux()
	api.Handle(prefix+"/", http.StripPrefix(prefix, http.HandlerFunc(gw.route)))

	root := http.NewServeMux()
	root.HandleFunc("/health", gw.handleHealth)
	root.Handle("/", gw.middlewareRateLimit(api))
	return gw.middlewareOrigin(root)
}

// handleHealth answers liveness probes. GET/HEAD only — accepting
// mutating verbs on /health would mislead probes that interpret 200
// on POST as "endpoint accepts writes".
func (gw *Gateway) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if gw.nats.IsConnected() {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","nats":"connected"}`))
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`{"status":"error","nats":"disconnected"}`))
}
