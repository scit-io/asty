package dashboard

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// leaderOnly is the middleware applied to every write (POST) handler.
// On a follower it reverse-proxies the request to the leader's
// dashboard listener and streams the response back. The X-Asty-Leader
// header is added so clients can still observe which node actually
// served the write.
//
// Why proxy instead of 307-redirect: browsers (and many proxy
// libraries) handle cross-origin POST redirects poorly — preflight,
// method downgrade, lost body. Server-side forwarding sidesteps that:
// the client sees exactly one same-origin POST and one final response.
// In dev (SPA on vite:5173 → dashboard:7060) the redirect would force
// the browser onto 127.0.0.2:7060 directly, triggering CORS; in prod
// the same hazard exists whenever the SPA isn't collocated with the
// leader. Proxying eliminates both.
//
// On a leaderless transient (LeaderInfo.IP empty) we return 503; no
// node can serve the write yet.
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
		// Same path on the leader's dashboard listener. Routes are
		// registered with the full prefix (see api.go: route()), so
		// r.URL.Path here already carries cfg.Dashboard.Prefix —
		// NewSingleHostReverseProxy's default Director forwards it
		// verbatim to the leader's mux, which matches on the same
		// pattern. We assume every server shares cfg.Dashboard.Port.
		port := api.cfg.Dashboard.Port
		target, err := url.Parse(fmt.Sprintf("http://%s:%d", info.IP, port))
		if err != nil {
			api.writeError(w, http.StatusInternalServerError, "invalid leader address", err)
			return
		}
		w.Header().Set("X-Asty-Leader", info.ID)
		httputil.NewSingleHostReverseProxy(target).ServeHTTP(w, r)
	}
}
