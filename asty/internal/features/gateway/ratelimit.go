// Rate limiting: two layers of incoming-traffic protection.
//
//  1. Per-IP path-prefix rules — declared by services in their .asty files.
//     Each rule specifies a URL prefix (e.g. "/<gateway-prefix>/<service>/")
//     with its own rate/burst. Longest matching prefix wins. Services can
//     declare as many rules as they need (brute-force defense, write
//     throttling, etc.).
//  2. Per-IP general cap — applies to all routes under the configured
//     gateway prefix that don't match any service-specific rule.
//  3. Global WS counter — caps simultaneous WebSocket connections.
//
// Algorithm: Token Bucket (golang.org/x/time/rate).
// IP table is swept every minute; entries idle >5 minutes drop. Size
// is bounded by gateway.rate_limit.max_ips (default 100 000); at the
// limit the LRU-oldest entry is evicted on insert.
package gateway

import (
	"container/list"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"asty/asty/internal/core/config"
	"asty/asty/internal/core/types"

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

// ipTable is a hash + LRU pair: O(1) lookup, O(1) eviction.
type ipTable struct {
	m   map[string]*ipEntry
	lst *list.List
}

func newIPTable() *ipTable {
	return &ipTable{m: make(map[string]*ipEntry), lst: list.New()}
}

// pathRule is a compiled service rate-limit rule with its own IP table.
type pathRule struct {
	prefix string
	rate   float64
	burst  int
	table  *ipTable
}

// rateLimiter holds the general table, per-path tables, and the WS counter.
type rateLimiter struct {
	mu      sync.Mutex
	general *ipTable
	rules   []pathRule // sorted longest-prefix-first for matching

	cfg     config.GatewayRateLimitConfig
	wsConns atomic.Int64
	log     zerolog.Logger
}

// newRateLimiter builds the limiter from gateway config + service-declared rules.
// done is the gateway context's Done channel — closing it stops cleanup.
func newRateLimiter(cfg config.GatewayRateLimitConfig, serviceRules []types.RateLimitRule, log zerolog.Logger, done <-chan struct{}) *rateLimiter {
	rules := make([]pathRule, 0, len(serviceRules))
	for _, sr := range serviceRules {
		if sr.PathPrefix == "" || sr.Rate <= 0 || sr.Burst <= 0 {
			continue
		}
		rules = append(rules, pathRule{
			prefix: sr.PathPrefix,
			rate:   sr.Rate,
			burst:  sr.Burst,
			table:  newIPTable(),
		})
	}
	// Longest prefix first — so "/<gw-prefix>/<service>/<method>" matches before "/<gw-prefix>/<service>/".
	sort.Slice(rules, func(i, j int) bool {
		return len(rules[i].prefix) > len(rules[j].prefix)
	})

	r := &rateLimiter{
		general: newIPTable(),
		rules:   rules,
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

// allowPath checks whether a service-specific rule applies to the path.
// Returns (true, "rule-name") if a rule matched and allowed,
// (false, "rule-name") if matched and rejected,
// (true, "") if no rule matched (fall through to general).
func (r *rateLimiter) allowPath(ip, path string) (allowed bool, matchedPrefix string) {
	for i := range r.rules {
		if strings.HasPrefix(path, r.rules[i].prefix) {
			ok := r.get(r.rules[i].table, ip, r.rules[i].rate, r.rules[i].burst).Allow()
			return ok, r.rules[i].prefix
		}
	}
	return true, ""
}

// get returns the limiter for ip, creating it on first contact.
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

// cleanup sweeps idle entries every minute.
func (r *rateLimiter) cleanup(done <-chan struct{}) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.mu.Lock()
			cutoff := time.Now().Add(-5 * time.Minute)
			r.cleanupExpired(r.general, cutoff)
			for i := range r.rules {
				r.cleanupExpired(r.rules[i].table, cutoff)
			}
			r.mu.Unlock()
		case <-done:
			return
		}
	}
}

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
