// Package health serves the orchestrator's GET /health liveness
// endpoint. Kept in its own package so kube-style probes and any other
// infrastructure can depend on it without pulling in api/rest's full
// surface; it has zero internal dependencies beyond stdlib.
package health

import (
	"encoding/json"
	"net/http"
	"time"
)

// Handler returns the http.Handler the router mounts at /health.
// Always replies 200 with {"status":"ok","timestamp":<unix>}; a probe
// that wants stronger liveness (e.g. "leader known") should check
// the data API separately. The body is JSON so a script can grep
// without parsing text formats.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":    "ok",
			"timestamp": time.Now().Unix(),
		})
	})
}
