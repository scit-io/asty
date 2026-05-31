package gateway

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"asty/asty/internal/core/types"
)

// clusterHostsCacheTTL bounds how stale the GET /api/v1 host-list
// response can be. The endpoint is hit by browser balancers and SSR
// nodes — refreshing more often than this would mean a KV listing per
// view without any real freshness gain (NodeInfo.Host only changes
// when an operator redeploys with a new A_NODE_HOST). The TTL also
// caps the load amplification a flood can inflict on KV.
const clusterHostsCacheTTL = 5 * time.Second

// hostsCacheEntry is the gateway-process-local cache of the public
// host list. Recomputed lazily under sync — at most one goroutine
// rebuilds, the rest read the previous slice. The slice is treated as
// immutable after publish, so concurrent readers don't need a copy.
type hostsCacheEntry struct {
	hosts    []string
	loadedAt time.Time
}

// handleClusterHosts serves GET /api/v1. Returns the public DNS names
// of every healthy node that has one. Designed for browser-side
// balancers and SSR nodes that need a list of live origins — same
// role A-records used to fill before we dropped DNS from peer
// discovery.
//
// Why minimal:
//   - host names only — no IPs, no node ids, no datacenter. Nothing
//     here that an attacker doesn't get from looking at a TLS handshake.
//   - GET only; HEAD answered for completeness.
//   - Anonymous: hosts are public DNS names by definition.
//   - The gateway's existing rate-limit and Origin allowlist still
//     apply (this handler sits behind both middlewares).
//   - In-process TTL cache caps KV load per node under a flood.
func (gw *Gateway) handleClusterHosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	hosts := gw.cachedClusterHosts()
	if hosts == nil {
		// json.Encode renders a nil []string as `null`; clients
		// expecting a JSON array should never see that.
		hosts = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	// Browsers may safely cache for the same window we hold internally,
	// since the rebuild interval is the freshness floor anyway.
	w.Header().Set("Cache-Control", "public, max-age=5")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_ = json.NewEncoder(w).Encode(hosts)
}

// cachedClusterHosts returns the most recent host list, recomputing
// when the TTL has expired. Recompute under the cache mutex so only
// one goroutine pays the KV cost during a stampede.
func (gw *Gateway) cachedClusterHosts() []string {
	gw.hostsCacheMu.Lock()
	defer gw.hostsCacheMu.Unlock()
	if time.Since(gw.hostsCache.loadedAt) < clusterHostsCacheTTL && gw.hostsCache.hosts != nil {
		return gw.hostsCache.hosts
	}
	hosts := gw.collectClusterHosts()
	gw.hostsCache = hostsCacheEntry{hosts: hosts, loadedAt: time.Now()}
	return hosts
}

// collectClusterHosts lists nodes from KV and projects healthy ones
// with a non-empty Host onto a sorted, de-duplicated string slice.
// Sorted output gives the cache a stable byte identity, which matters
// for downstream CDN caching.
func (gw *Gateway) collectClusterHosts() []string {
	if gw.clusterState == nil {
		return []string{}
	}
	nodes, err := gw.clusterState.ListNodes()
	if err != nil {
		gw.log.Warn().Err(err).Msg("cluster-hosts: ListNodes failed; serving previous cache (or empty)")
		return gw.hostsCache.hosts
	}
	now := time.Now()
	seen := make(map[string]struct{}, len(nodes))
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n == nil || n.Host == "" {
			continue
		}
		if !nodeIsServingTraffic(n, now) {
			continue
		}
		if _, dup := seen[n.Host]; dup {
			continue
		}
		seen[n.Host] = struct{}{}
		out = append(out, n.Host)
	}
	sort.Strings(out)
	return out
}

// nodeIsServingTraffic is the gateway's own definition of "OK to send
// users here". Mirrors types.NodeInfo.IsHealthy (live heartbeat) and
// adds the status filter: drained / paused / down nodes shouldn't
// appear in a balancer's pool.
func nodeIsServingTraffic(n *types.NodeInfo, now time.Time) bool {
	if !n.IsHealthy(now) {
		return false
	}
	switch n.EffectiveStatus(now) {
	case types.NodeReady:
		return true
	default:
		return false
	}
}
