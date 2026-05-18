package types

import "time"

// AuditEvent is the payload published on asty.v1.audit.<resource>.<action>
// for every write to the dashboard API. Operators stream the subject
// out of NATS into whatever audit store they keep (Vector → BigQuery,
// Loki, etc.); Asty itself does not retain audit history beyond the
// in-memory ring buffer the dashboard surfaces for live tailing.
//
// Schema-stable: new fields are appended (CBOR-friendly), existing
// fields keep their JSON keys. Token identity is deliberately coarse
// — Asty does not yet model per-operator accounts, so all we record
// is "the request carried a valid token". A future user-account
// migration will populate Actor.
type AuditEvent struct {
	Timestamp int64     `json:"timestamp"`              // unix seconds at handler exit
	Method    string    `json:"method"`                 // HTTP method
	Path      string    `json:"path"`                   // dashboard-prefix-stripped path
	Resource  string    `json:"resource"`               // first path segment (nodes, services, …)
	Action    string    `json:"action"`                 // canonical verb (drain, deploy, scale, …)
	Status    int       `json:"status"`                 // HTTP response status code
	NodeID    string    `json:"node_id,omitempty"`      // when the route targets a specific node
	Service   string    `json:"service,omitempty"`      // when the route targets a specific service
	AllocID   string    `json:"alloc_id,omitempty"`     // when the route targets a specific allocation
	ActorIP   string    `json:"actor_ip,omitempty"`     // client address (request RemoteAddr, post X-Forwarded-For)
	RequestID string    `json:"request_id,omitempty"`   // X-Request-Id echo, when the client sets one
	At        time.Time `json:"at"`                     // RFC3339 form of Timestamp, for human grep
}

// AuditSubjectRoot is the NATS subject prefix every AuditEvent is
// published under. Subscribers usually wildcard `asty.v1.audit.>`.
const AuditSubjectRoot = "asty.v1.audit"
