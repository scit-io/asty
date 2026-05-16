// Package gateway is the HTTP/WebSocket entry point embedded in the
// asty agent process. It proxies HTTP requests and upgrades WebSocket
// connections to NATS, using the agent's own NATS connection — no
// extra process, no extra connection.
package gateway

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"asty/asty/internal/core/config"
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
	allowedHosts allowedHostSet
	rl           *rateLimiter
	log          zerolog.Logger

	// validRequests counts /v1/* requests that survived the Origin and
	// rate-limit middlewares — what the autoscaler treats as real
	// traffic on this node. Sampled by reportRPSLoop.
	validRequests atomic.Int64

	// ctx is the gateway-scoped context. Cancelled when the parent
	// context (the agent's) is cancelled, which lets WS handlers and
	// HTTP requests observe a single shutdown signal.
	ctx    context.Context
	cancel context.CancelFunc
}

// New builds a gateway bound to nc and cfg. nodeID is the agent's
// own node id, used as the suffix of the RPS report subject so the
// server can attribute traffic per node. serviceRules are rate-limit
// rules collected from all loaded .asty service definitions — the
// gateway enforces them on incoming requests before proxying to NATS.
func New(parent context.Context, nc *nats.Conn, cfg config.GatewayConfig, nodeID string, serviceRules []types.RateLimitRule, log zerolog.Logger) (*Gateway, error) {
	hosts, err := parseAllowedHosts(log, cfg.AllowedHosts)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(parent)
	gw := &Gateway{
		nats:         nc,
		cfg:          cfg,
		nodeID:       nodeID,
		allowedHosts: hosts,
		rl:           newRateLimiter(cfg.RateLimit, serviceRules, log, ctx.Done()),
		log:          log,
		ctx:          ctx,
		cancel:       cancel,
	}
	gw.upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return hosts.allows(log, r.Header.Get("Origin"))
		},
	}
	return gw, nil
}

// RootContext returns a context that tracks gateway shutdown. Wire it
// into http.Server.BaseContext so each request's r.Context() cancels
// on shutdown, letting NATS round-trips abort promptly.
func (gw *Gateway) RootContext() context.Context { return gw.ctx }

// Shutdown cancels the gateway's context. Triggers WS sessions to
// close and unblocks any callers blocked on RootContext().Done().
func (gw *Gateway) Shutdown() { gw.cancel() }

// Handler returns the root http.Handler of the gateway.
//
// Routes:
//   - /health — health check (no rate limit, no Origin check, no NATS proxy)
//   - /v1/    — API: Origin → RateLimit → HTTP RPC or WebSocket
func (gw *Gateway) Handler() http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("/v1/", gw.route)

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
