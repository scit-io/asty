// Three layers of incoming-traffic limits:
//  1. Per-IP HTTP rate limit — general cap for all /v1/ routes.
//  2. Per-IP auth rate limit — extra strict cap for a configurable URL
//     prefix (gateway.rate_limit.auth_path_prefix); brute-force defense.
//     Empty prefix disables the second tier.
//  3. Global WS counter — caps simultaneous WebSocket connections.
//
// Algorithm: Token Bucket (golang.org/x/time/rate).
// IP table is swept every minute; entries idle >5 minutes drop. Size
// is bounded by gateway.rate_limit.max_ips (default 100 000); at the
// limit the LRU-oldest entry is evicted on insert.
package gateway

import (
	"container/list"
	"sync"
	"sync/atomic"
	"time"

	"asty/internal/platform/asty/core/config"

	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
)

// ipEntry is one row in the per-IP table. elem points to the node
// in the LRU list — accesses move it to the front, full-table inserts
// drop from the back.
type ipEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
	elem     *list.Element
}

// ipTable is a hash + LRU pair: O(1) lookup, O(1) eviction. Invariant:
// len(m) == lst.Len(); the list is sorted by lastSeen, back oldest.
type ipTable struct {
	m   map[string]*ipEntry
	lst *list.List
}

func newIPTable() *ipTable {
	return &ipTable{m: make(map[string]*ipEntry), lst: list.New()}
}

// rateLimiter holds both per-IP tables plus the global WS counter.
type rateLimiter struct {
	mu      sync.Mutex
	general *ipTable
	auth    *ipTable

	cfg     config.GatewayRateLimitConfig
	wsConns atomic.Int64
	log     zerolog.Logger
}

// newRateLimiter builds the limiter and spawns the background cleanup
// goroutine. done is the gateway context's Done channel — closing it
// stops cleanup and releases the goroutine.
func newRateLimiter(cfg config.GatewayRateLimitConfig, log zerolog.Logger, done <-chan struct{}) *rateLimiter {
	r := &rateLimiter{
		general: newIPTable(),
		auth:    newIPTable(),
		cfg:     cfg,
		log:     log,
	}
	go r.cleanup(done)
	return r
}

// allow checks the general per-IP cap.
func (r *rateLimiter) allow(ip string) bool {
	return r.get(r.general, ip, r.cfg.Rate, r.cfg.Burst).Allow()
}

// allowAuth checks the strict per-IP cap for auth routes.
func (r *rateLimiter) allowAuth(ip string) bool {
	return r.get(r.auth, ip, r.cfg.AuthRate, r.cfg.AuthBurst).Allow()
}

// get returns the limiter for ip, creating it on first contact.
// Hit moves the entry to LRU front (O(1)). Miss with a full table
// drops the LRU back entry first (O(1)) instead of a linear scan.
// Under DDoS from unique IPs each request spends microseconds under
// the mutex regardless of table size.
func (r *rateLimiter) get(table *ipTable, ip string, ratePerSec float64, burst int) *rate.Limiter {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if e, ok := table.m[ip]; ok {
		e.lastSeen = now
		table.lst.MoveToFront(e.elem)
		return e.limiter
	}

	if len(table.m) >= r.cfg.MaxIPs {
		if oldest := table.lst.Back(); oldest != nil {
			oldIP := oldest.Value.(string)
			table.lst.Remove(oldest)
			delete(table.m, oldIP)
		}
	}

	e := &ipEntry{
		limiter:  rate.NewLimiter(rate.Limit(ratePerSec), burst),
		lastSeen: now,
	}
	e.elem = table.lst.PushFront(ip)
	table.m[ip] = e
	return e.limiter
}

// cleanup sweeps idle entries every minute. An entry counts as idle
// if its lastSeen is older than 5 minutes.
func (r *rateLimiter) cleanup(done <-chan struct{}) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.mu.Lock()
			cutoff := time.Now().Add(-5 * time.Minute)
			r.cleanupExpired(r.general, cutoff)
			r.cleanupExpired(r.auth, cutoff)
			r.mu.Unlock()
		case <-done:
			return
		}
	}
}

// cleanupExpired walks from the LRU back removing entries whose
// lastSeen is before cutoff. Stops at the first fresh entry — the
// sort invariant guarantees the rest are fresher. O(K) for K
// actually-expired rather than O(N). Caller holds r.mu.
func (r *rateLimiter) cleanupExpired(table *ipTable, cutoff time.Time) {
	for {
		oldest := table.lst.Back()
		if oldest == nil {
			return
		}
		ip := oldest.Value.(string)
		if !table.m[ip].lastSeen.Before(cutoff) {
			return
		}
		table.lst.Remove(oldest)
		delete(table.m, ip)
	}
}
