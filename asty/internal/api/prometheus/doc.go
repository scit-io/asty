// Package prom serves the Prometheus exposition endpoint of the
// orchestrator (`GET /metrics`, exact). All Asty-specific collectors
// live here; the package is independent of api/rest so it can be
// mounted on its own listener if a future deployment wants to keep
// the scrape target away from the data API. For now the same
// http.Server mounts prom.Handler at /metrics and api/rest at /api/v1.
//
// The package defines a minimal Context interface listing exactly the
// reads it performs on the surrounding system; server passes itself
// as Context via Go's structural typing without touching api/rest's
// fuller ServerContext.
package prometheus
