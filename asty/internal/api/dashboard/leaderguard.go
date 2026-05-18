package dashboard

import (
	"fmt"
	"net/http"
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
		// Redirect target: same path on the leader's dashboard
		// listener. We assume every server in the cluster shares
		// cfg.Dashboard.Port (the usual deployment pattern); cross-port
		// topologies would need to publish the port into LeaderInfo.
		// Path is preserved verbatim — cfg.Dashboard.Prefix is already
		// part of r.URL.Path here, so the redirect points at the same
		// data route.
		port := api.cfg.Dashboard.Port
		target := fmt.Sprintf("http://%s:%d%s", info.IP, port, r.URL.RequestURI())
		w.Header().Set("Location", target)
		w.Header().Set("X-Asty-Leader", info.ID)
		http.Error(w, fmt.Sprintf("not leader, redirect to %s", info.ID), http.StatusTemporaryRedirect)
	}
}

// (apiPortFromAddr removed — the redirect now reads cfg.Dashboard.Port
// directly, no string parsing needed.)
