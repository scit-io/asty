package agent

import (
	"context"
	"sort"
	"strings"
	"sync"

	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
)

// natsPeers holds the IPs of OTHER cluster nodes, fed from two
// independent maps and merged at snapshot time:
//
//   - byNode (nodeID → IP): the steady-state view, maintained by
//     watchNATSPeers from cluster-KV WatchNodes. Every fully-joined
//     node lives here, indexed by its node ID.
//   - bootstrap (IP set): IPs known before a peer has a node ID we
//     can use. Three things write here: cfg.NATS.Seed at agent
//     construction (operator-provided live-node hint), CmdAddPeer
//     from a local SSH'd `asty admin add-peer`, and the peer-announce
//     subscriber that mirrors that command on every other node. Once
//     the peer fully joins and registers in KV, watchNATSPeers adds
//     the same IP under its proper node ID; the entry appears in both
//     maps until then, and snapshot() de-dupes.
//
// The bootstrap set is what breaks the standalone→clustered cold-start
// deadlock: a live node in standalone mode (no cluster{} block, port
// 6222 closed) gets the incoming peer added via CmdAddPeer, then the
// supervisor cold-restarts nats-server with a proper cluster{} block.
// By the time the new node tries to route to us, :6222 is open.
type natsPeers struct {
	mu        sync.RWMutex
	byNode    map[string]string   // nodeID → IP (from cluster KV)
	bootstrap map[string]struct{} // IPs from Seed / CmdAddPeer / peer-announce, before KV catches up
}

func newNATSPeers() *natsPeers {
	return &natsPeers{
		byNode:    make(map[string]string),
		bootstrap: make(map[string]struct{}),
	}
}

// snapshot returns the union of byNode + bootstrap, sorted ascending.
// Sort + unique gives deterministic output so two renders of the same
// membership produce byte-identical nats.conf — what hot-reload change
// detection relies on.
func (p *natsPeers) snapshot() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	all := make(map[string]struct{}, len(p.byNode)+len(p.bootstrap))
	for _, ip := range p.byNode {
		all[ip] = struct{}{}
	}
	for ip := range p.bootstrap {
		all[ip] = struct{}{}
	}
	out := make([]string, 0, len(all))
	for ip := range all {
		out = append(out, ip)
	}
	sort.Strings(out)
	return out
}

// countByNode returns the number of OTHER cluster nodes known via
// cluster-KV WatchNodes. The quorum-lost collapse gate uses it to tell a
// 2-node cluster (≤1 other node) from a larger one — never collapse a
// partitioned minority at N≥3.
func (p *natsPeers) countByNode() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.byNode)
}

// hasBootstrap reports whether any peer is still pending in the bootstrap
// set — an IP from Seed / CmdAddPeer / peer-announce that has NOT yet
// appeared in cluster KV (byNode). A non-empty bootstrap set means a peer
// is JOINING: this node just cold-restarted clustered to accept it and is
// waiting for it to connect (a 1→N re-grow). The natssolo 2→1 collapse
// uses this to tell a grow from a shrink — during a grow the transient
// no-quorum is the joiner not-yet-connected, NOT a peer loss, so collapsing
// would slam :6222 shut before the joiner gets in. A genuine shrink leaves
// bootstrap empty: remove() drops a departed peer's IP, and a solo survivor
// went through reset().
func (p *natsPeers) hasBootstrap() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.bootstrap) > 0
}

// bootstrapSnapshot returns the current bootstrap IP set, suitable for
// iteration without holding the lock.
func (p *natsPeers) bootstrapSnapshot() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]string, 0, len(p.bootstrap))
	for ip := range p.bootstrap {
		out = append(out, ip)
	}
	return out
}

// allPeerIPs returns the union of byNode and bootstrap IPs — every
// "other peer" this node knows about, regardless of source. The
// natssolo gate uses this for TCP-reachability probes so a stale
// byNode (KV-watch missed a delete) does not block the collapse.
func (p *natsPeers) allPeerIPs() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	all := make(map[string]struct{}, len(p.byNode)+len(p.bootstrap))
	for _, ip := range p.byNode {
		all[ip] = struct{}{}
	}
	for ip := range p.bootstrap {
		all[ip] = struct{}{}
	}
	out := make([]string, 0, len(all))
	for ip := range all {
		out = append(out, ip)
	}
	return out
}

// dropBootstrap removes a single IP from the bootstrap set. Returns
// whether the IP was actually present.
func (p *natsPeers) dropBootstrap(ip string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.bootstrap[ip]; !ok {
		return false
	}
	delete(p.bootstrap, ip)
	return true
}

// reset clears every known peer so the next render produces a standalone
// (no cluster{} block) conf. Used by the solo transition when this node
// becomes the last one.
func (p *natsPeers) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.byNode = make(map[string]string)
	p.bootstrap = make(map[string]struct{})
}

// upsert records nodeID→ip and returns whether the IP set changed.
// Updating an existing node's IP to the same value is a no-op signal-
// wise; a new node or a changed IP both count as a change.
func (p *natsPeers) upsert(nodeID, ip string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if prev, ok := p.byNode[nodeID]; ok && prev == ip {
		return false
	}
	p.byNode[nodeID] = ip
	return true
}

func (p *natsPeers) remove(nodeID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	ip, ok := p.byNode[nodeID]
	if !ok {
		return false
	}
	delete(p.byNode, nodeID)
	// Also drop the IP from the bootstrap set. A departed node's IP must
	// stop being rendered into cluster.routes — otherwise a seed node
	// that leaves (its IP sits in every other node's bootstrap set via
	// A_NATS_SEED) gets dialed forever: "connect to route ...:6222:
	// connection refused" on a loop, the exact noise a killed seed left
	// across the cluster. Safe because once KV knows every peer the
	// bootstrap hint is redundant; dropping a dead one loses nothing.
	delete(p.bootstrap, ip)
	return true
}

// addBootstrap records an incoming-peer IP from a CmdAddPeer call.
// Returns true if the IP wasn't already known (so the supervisor
// should signal a restart). Filters self-IP so an accidental loopback
// announce stays a no-op.
func (p *natsPeers) addBootstrap(ip, selfIP string) bool {
	if ip == "" || ip == selfIP {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	// Already known via either channel — skip.
	if _, ok := p.bootstrap[ip]; ok {
		return false
	}
	for _, existing := range p.byNode {
		if existing == ip {
			return false
		}
	}
	p.bootstrap[ip] = struct{}{}
	return true
}

// seedPeers parses cfg.NATS.Seed (sourced from A_NATS_SEED) into a
// list of peer IPs. Called once by agent.New() to populate the
// bootstrap-peer set BEFORE NATS is started, so the very first
// renderNATSConf includes the operator-provided live-node hint.
// Self-IP is filtered so an operator that accidentally lists this
// node's own IP gets a clean conf. Accepts a comma-separated list —
// single IP is the documented form, but the parser is liberal
// because it costs nothing.
func seedPeers(seed, selfIP string) []string {
	if seed == "" {
		return nil
	}
	out := make([]string, 0, 1)
	for _, raw := range strings.Split(seed, ",") {
		s := strings.TrimSpace(raw)
		if s == "" || s == selfIP {
			continue
		}
		out = append(out, s)
	}
	return out
}

// watchNATSPeers is the steady-state peer source: it subscribes to
// WatchNodes (KV) and signals the supervisor whenever the resolved
// peer IP set changes. Event-driven (no polling), so a node join or
// leave propagates as fast as KV history replicates — typically
// <100 ms.
//
// Bootstrap-time peers come from cfg.NATS.Seed (filled in agent.New()
// before NATS starts) and from CmdAddPeer (incoming SSH'd announces);
// this watcher takes over after connectAndWireNATS, when the cluster
// KV is reachable. The first WatchNodes replay backfills a.peers with
// every already-known node, signalling a restart only if it actually
// adds something the bootstrap set didn't have.
func (a *Agent) watchNATSPeers(ctx context.Context) {
	if a.clusterState == nil {
		log.Warn().Msg("nats peer watcher disabled: cluster state not initialized")
		return
	}
	selfIP := a.resolveNodeIP()
	if selfIP == "" {
		log.Warn().Msg("nats peer watcher disabled: cannot resolve local node IP")
		return
	}

	onChange := func(node *types.NodeInfo) {
		if node == nil || node.ID == "" || node.ID == a.nodeID {
			return
		}
		isDelete := node.Status == types.NodeDeleted || node.IP == ""
		// In solo recovery mode, drop late upsert events: KV-watch can
		// keep delivering stale records of now-dead peers for a while
		// after natssolo collapses us to standalone, and re-adding them
		// to byNode triggers a spurious cold-restart back into clustered
		// mode with cluster.routes pointing at dead IPs. Delete events
		// (peer cleanup) still go through so byNode drains naturally.
		if !isDelete && a.inSolo.Load() {
			return
		}
		var changed bool
		if isDelete {
			changed = a.peers.remove(node.ID)
		} else {
			changed = a.peers.upsert(node.ID, node.IP)
		}
		if !changed {
			return
		}
		log.Debug().Str("node_id", node.ID).Str("ip", node.IP).
			Strs("peers", a.peers.snapshot()).
			Msg("nats peers changed (from cluster KV)")
		a.signalNATSRestart()
	}

	if err := a.clusterState.WatchNodes(ctx, onChange); err != nil {
		log.Warn().Err(err).Msg("nats peer watcher exited with error")
	}
}

// signalNATSRestart is a buffered, drop-on-full notify. A second tick
// before the supervisor services the first is harmless: the restart
// already picks up the freshest peer list.
func (a *Agent) signalNATSRestart() {
	select {
	case a.natsRestartCh <- struct{}{}:
	default:
	}
}
