package api

import (
	"fmt"
	"net/http"
	"strings"
)

// leaderOnly is the middleware applied to every write (POST) handler.
// On a follower it issues 307 Temporary Redirect to the same path on
// the current leader's API address; the client (UI, CLI, ops script)
// can follow without code changes. Returning 307 (not 302) preserves
// the original method and body — POST stays POST after the redirect.
//
// We use a 307 even on leaderless transients (LeaderInfo.IP empty):
// in that case Location is left out and the body explains the
// situation; the client gets 503, since no node can serve the write.
//
// Centralising this in one place replaces the ad-hoc IsLeader checks
// previously scattered across services.go and lets us treat
// leader-only as a property of routes, not handlers.
func (api *API) leaderOnly(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		le := api.ctx.LeaderElection()
		if le == nil {
			api.writeError(w, http.StatusServiceUnavailable, "leader election not initialised", nil)
			return
		}
		if le.IsLeader() {
			h(w, r)
			return
		}
		info, _ := le.GetLeader()
		if info.IP == "" {
			api.writeError(w, http.StatusServiceUnavailable, "no leader currently known", nil)
			return
		}
		// Redirect target: same path on leader's API listener. We assume
		// every server in the cluster shares cfg.HTTP.Addr's port (the
		// usual deployment pattern); cross-port topologies would need to
		// publish the API port into LeaderInfo. Path is preserved
		// verbatim — apiPrefix is already part of r.URL.Path here, so
		// the redirect points at the same data route.
		port := apiPortFromAddr(api.addr)
		target := fmt.Sprintf("http://%s:%s%s", info.IP, port, r.URL.RequestURI())
		w.Header().Set("Location", target)
		w.Header().Set("X-Asty-Leader", info.ID)
		http.Error(w, fmt.Sprintf("not leader, redirect to %s", info.ID), http.StatusTemporaryRedirect)
	}
}

// apiPortFromAddr extracts the port from a listen address like
// ":8080" or "127.0.0.1:8080". Returns "8080" as a safe default if
// the input doesn't parse — the redirect Location will still be
// valid against the standard deployment.
func apiPortFromAddr(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 && i+1 < len(addr) {
		return addr[i+1:]
	}
	return "8080"
}
