package gateway

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"asty/asty/internal/core/types"
)

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
//   - No caching of any kind: every request hits KV. The list has to
//     reflect the live cluster, and KV listing under the gateway's
//     rate-limit ceiling is cheap.
func (gw *Gateway) handleClusterHosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	hosts := gw.collectClusterHosts()

	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_ = json.NewEncoder(w).Encode(hosts)
}

// collectClusterHosts lists nodes from KV and projects healthy ones
// with a non-empty Host onto a sorted, de-duplicated string slice.
// Returns a non-nil slice in every branch so json.Encode never emits
// `null` for clients expecting an array.
func (gw *Gateway) collectClusterHosts() []string {
	if gw.clusterState == nil {
		return []string{}
	}
	nodes, err := gw.clusterState.ListNodes()
	if err != nil {
		gw.log.Warn().Err(err).Msg("cluster-hosts: ListNodes failed; serving empty list")
		return []string{}
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
