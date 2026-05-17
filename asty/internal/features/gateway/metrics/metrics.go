// Package metrics holds the Prometheus instruments emitted by the
// embedded gateway. Exposed through the /metrics endpoint started by
// the agent (see agent/gateway.go).
//
// All instruments register via promauto into the default registry —
// repeated imports do not double-register and Go runtime metrics
// (`go_*`, `process_*`) are picked up automatically.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// HTTPRequestsTotal counts HTTP requests proxied through the gateway.
//
// The `service` and `method` labels come from the URL after routing.
// To stop /v1/{rand}/{rand} from blowing up cardinality (the regex
// `validSubjectToken` allows any pair of tokens, not only real
// backends), after ErrNoResponders outlives the retry window the
// labels collapse to service="unknown",method="unknown". Known pairs
// without subscribers (rolling deploy) end up there too — retries
// absorb short deploy windows; only a truly dead subject reaches
// this counter as ErrNoResponders.
var HTTPRequestsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gateway_http_requests_total",
		Help: "Total HTTP requests proxied through Gateway by service, method, status.",
	},
	[]string{"service", "method", "status"},
)

// HTTPRequestDuration measures end-to-end HTTP duration (request in →
// response written). Default Prometheus buckets (5ms..10s) match the
// typical NATS-proxied range. Label semantics mirror HTTPRequestsTotal:
// "unknown" collapse applies on terminal ErrNoResponders.
var HTTPRequestDuration = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name: "gateway_http_request_duration_seconds",
		Help: "End-to-end Gateway HTTP request duration in seconds, by service, method.",
	},
	[]string{"service", "method"},
)

// WSConnectionsActive is the count of currently-open WebSocket
// sessions. wsConnGuard increments on accept; the matching release
// decrements on close.
var WSConnectionsActive = promauto.NewGauge(
	prometheus.GaugeOpts{
		Name: "gateway_ws_connections_active",
		Help: "Number of currently open WebSocket connections to Gateway.",
	},
)

// RateLimitRejectedTotal counts requests rejected by the gateway rate
// limiter. The `kind` label takes either `general` (the per-IP fallback
// cap) or the path prefix of the matched service rate-limit rule.
var RateLimitRejectedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gateway_rate_limit_rejected_total",
		Help: "HTTP requests rejected by Gateway rate limiter. The `kind` label is `general` for the per-IP fallback cap, otherwise the matched path prefix from a service's rate-limit rule.",
	},
	[]string{"kind"},
)

// NATSRequestDuration measures the NATS Request-Reply round trip
// from the gateway (retries included). The `service` label collapses
// to "unknown" on terminal ErrNoResponders, symmetric with HTTPRequestsTotal.
var NATSRequestDuration = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name: "nats_request_duration_seconds",
		Help: "Duration of NATS Request-Reply round-trip from Gateway, by service.",
	},
	[]string{"service"},
)

// NATSRequestAttemptsTotal counts attempts per NATS request — a
// single HTTP request increments it by the number of attempts; the
// `outcome` label captures the final result. The `service` label
// collapses to "unknown" on terminal ErrNoResponders.
var NATSRequestAttemptsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "nats_request_attempts_total",
		Help: "Total NATS Request-Reply attempts from Gateway, by service, outcome (ok|no_responders|timeout|error).",
	},
	[]string{"service", "outcome"},
)
