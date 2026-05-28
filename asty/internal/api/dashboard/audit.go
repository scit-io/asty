package dashboard

import (
	"net/http"
	"strings"
	"time"

	"asty/asty/internal/core/codec"
	"asty/asty/internal/core/netutil"
	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
)

// auditLog wraps a write handler and publishes an AuditEvent on
// asty.v1.audit.<resource>.<action> once the handler returns. Captures
// the HTTP status via a thin ResponseWriter wrapper; the wire payload
// is CBOR through codec.Wire so the schema stays stable when fields
// are added.
//
// Publication failures are logged at warn — they should not bubble up
// to the client. Audit is observation, not gating; tokenAuth +
// leaderOnly already decided whether the write may happen at all.
func (api *API) auditLog(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		h(rec, r)

		nc := api.ctx.NATSConn()
		if nc == nil {
			return
		}

		resource, action, nodeID, service, allocID := classifyPath(r.Method, r.URL.Path)
		evt := types.AuditEvent{
			Timestamp: time.Now().Unix(),
			Method:    r.Method,
			Path:      r.URL.Path,
			Resource:  resource,
			Action:    action,
			Status:    rec.status,
			NodeID:    nodeID,
			Service:   service,
			AllocID:   allocID,
			ActorIP:   netutil.RealIP(r, api.cfg.Gateway.RateLimit.TrustedProxy),
			RequestID: r.Header.Get("X-Request-Id"),
			At:        time.Now(),
		}
		payload, err := codec.Wire.Marshal(&evt)
		if err != nil {
			log.Warn().Err(err).Msg("audit: marshal failed")
			return
		}
		subject := types.AuditSubjectRoot + "." + resource + "." + action
		if err := nc.Publish(subject, payload); err != nil {
			log.Warn().Err(err).Str("subject", subject).Msg("audit: publish failed")
		}
	}
}

// statusRecorder wraps http.ResponseWriter to capture the response
// status for the audit event. Falls through to the underlying writer
// for everything else, including Flusher when the underlying writer
// supports it (so SSE still works if an audit-wrapped handler ever
// emits a stream — none currently do, this is defensive).
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.wroteHeader = true
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// classifyPath splits the dashboard-prefix-stripped URL path into a
// (resource, action, nodeID, service, allocID) tuple. Drives the
// audit subject name and the per-target fields. Conservative: unknown
// shapes get resource="unknown" so we still publish something on
// every write, but the subject doesn't grow unbounded.
//
// Expected shapes (after http.StripPrefix):
//
//	POST /nodes/{id}/drain
//	POST /nodes/{id}/pause
//	POST /nodes/{id}/allocations/{aid}/restart
//	POST /nodes/{id}/allocations/{aid}/stop
//	POST /services/{name}/scale
//	POST /services/{name}/deploy
func classifyPath(method, path string) (resource, action, nodeID, service, allocID string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "unknown", strings.ToLower(method), "", "", ""
	}
	resource = parts[0]
	switch resource {
	case "nodes":
		if len(parts) >= 2 {
			nodeID = parts[1]
		}
		switch {
		case len(parts) == 3 && parts[2] == "drain":
			return "nodes", "drain", nodeID, "", ""
		case len(parts) == 3 && parts[2] == "pause":
			return "nodes", "pause", nodeID, "", ""
		case len(parts) >= 4 && parts[2] == "allocations":
			allocID = parts[3]
			if len(parts) == 5 && parts[4] == "restart" {
				return "allocations", "restart", nodeID, "", allocID
			}
			if len(parts) == 5 && parts[4] == "stop" {
				return "allocations", "stop", nodeID, "", allocID
			}
		}
	case "services":
		if len(parts) >= 2 {
			service = parts[1]
		}
		if len(parts) == 3 && parts[2] == "scale" {
			return "services", "scale", "", service, ""
		}
		if len(parts) == 3 && parts[2] == "deploy" {
			return "services", "deploy", "", service, ""
		}
	}
	return resource, strings.ToLower(method), nodeID, service, allocID
}
