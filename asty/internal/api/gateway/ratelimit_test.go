package gateway

import (
	"io"
	"net/http"
	"testing"
	"time"

	"asty/asty/internal/core/config"
	"asty/asty/internal/core/types"

	"github.com/rs/zerolog"
)

func newTestRL(maxIPs int) *rateLimiter {
	done := make(chan struct{})
	close(done)
	return newRateLimiter(
		config.GatewayRateLimitConfig{
			Rate:       100,
			Burst:      10,
			MaxWSConns: 1000,
			MaxIPs:     maxIPs,
		},
		nil,
		zerolog.New(io.Discard),
		done,
	)
}

func newTestRLWithRules(maxIPs int, rules []types.RateLimitRule) *rateLimiter {
	done := make(chan struct{})
	close(done)
	return newRateLimiter(
		config.GatewayRateLimitConfig{
			Rate:       100,
			Burst:      10,
			MaxWSConns: 1000,
			MaxIPs:     maxIPs,
		},
		rules,
		zerolog.New(io.Discard),
		done,
	)
}

func TestRL_GetCreatesEntryAtFront(t *testing.T) {
	r := newTestRL(10)
	r.get(r.general, "1.1.1.1", 100, 10)

	if len(r.general.m) != 1 {
		t.Fatalf("map size = %d, want 1", len(r.general.m))
	}
	if r.general.lst.Front().Value.(string) != "1.1.1.1" {
		t.Errorf("front = %v, want 1.1.1.1", r.general.lst.Front().Value)
	}
}

func TestRL_GetMovesAccessedEntryToFront(t *testing.T) {
	r := newTestRL(10)

	r.get(r.general, "1.1.1.1", 100, 10)
	r.get(r.general, "2.2.2.2", 100, 10)
	r.get(r.general, "3.3.3.3", 100, 10)

	if r.general.lst.Back().Value.(string) != "1.1.1.1" {
		t.Errorf("back = %v, want 1.1.1.1", r.general.lst.Back().Value)
	}

	r.get(r.general, "1.1.1.1", 100, 10)
	if r.general.lst.Front().Value.(string) != "1.1.1.1" {
		t.Errorf("after re-access, front = %v, want 1.1.1.1", r.general.lst.Front().Value)
	}
	if r.general.lst.Back().Value.(string) != "2.2.2.2" {
		t.Errorf("after re-access, back = %v, want 2.2.2.2", r.general.lst.Back().Value)
	}
}

func TestRL_LRUEvictsOldestWhenFull(t *testing.T) {
	r := newTestRL(3)

	r.get(r.general, "1.1.1.1", 100, 10)
	r.get(r.general, "2.2.2.2", 100, 10)
	r.get(r.general, "3.3.3.3", 100, 10)

	if len(r.general.m) != 3 {
		t.Fatalf("map size after fill = %d, want 3", len(r.general.m))
	}

	r.get(r.general, "4.4.4.4", 100, 10)

	if len(r.general.m) != 3 {
		t.Errorf("map size after eviction = %d, want 3", len(r.general.m))
	}
	if _, ok := r.general.m["1.1.1.1"]; ok {
		t.Errorf("oldest IP 1.1.1.1 should have been evicted")
	}
	if _, ok := r.general.m["4.4.4.4"]; !ok {
		t.Errorf("new IP 4.4.4.4 should be present")
	}
}

func TestRL_LimiterReusedForSameIP(t *testing.T) {
	r := newTestRL(10)
	l1 := r.get(r.general, "1.1.1.1", 100, 10)
	l2 := r.get(r.general, "1.1.1.1", 100, 10)
	if l1 != l2 {
		t.Errorf("same IP returned different limiters")
	}
}

func TestRL_CleanupExpired(t *testing.T) {
	r := newTestRL(10)
	now := time.Now()

	r.get(r.general, "old1", 100, 10)
	r.general.m["old1"].lastSeen = now.Add(-10 * time.Minute)

	r.get(r.general, "old2", 100, 10)
	r.general.m["old2"].lastSeen = now.Add(-7 * time.Minute)

	r.get(r.general, "fresh", 100, 10)
	r.general.m["fresh"].lastSeen = now

	r.cleanupExpired(r.general, now.Add(-5*time.Minute))

	if _, ok := r.general.m["old1"]; ok {
		t.Errorf("old1 should be removed")
	}
	if _, ok := r.general.m["old2"]; ok {
		t.Errorf("old2 should be removed")
	}
	if _, ok := r.general.m["fresh"]; !ok {
		t.Errorf("fresh entry should remain")
	}
}

func TestRL_CleanupStopsAtFirstFresh(t *testing.T) {
	r := newTestRL(10)
	now := time.Now()

	r.get(r.general, "fresh", 100, 10)
	r.general.m["fresh"].lastSeen = now

	r.cleanupExpired(r.general, now.Add(-5*time.Minute))

	if _, ok := r.general.m["fresh"]; !ok {
		t.Errorf("fresh entry incorrectly removed")
	}
}

func TestRL_AllowPathMatchesLongestPrefix(t *testing.T) {
	rules := []types.RateLimitRule{
		{PathPrefix: "/v1/foo/", Rate: 5, Burst: 1},
		{PathPrefix: "/v1/foo/bar", Rate: 2, Burst: 1},
	}
	r := newTestRLWithRules(10, rules)

	// "/v1/foo/bar" should match the more specific rule (rate=2, burst=1)
	allowed, prefix := r.allowPath("1.1.1.1", "/v1/foo/bar")
	if prefix != "/v1/foo/bar" {
		t.Errorf("matched prefix = %q, want /v1/foo/bar", prefix)
	}
	if !allowed {
		t.Error("first request should be allowed")
	}

	// Second request should be rejected (burst=1 exhausted)
	allowed, _ = r.allowPath("1.1.1.1", "/v1/foo/bar")
	if allowed {
		t.Error("second request should be rejected (burst=1)")
	}
}

func TestRL_AllowPathNoMatch(t *testing.T) {
	rules := []types.RateLimitRule{
		{PathPrefix: "/v1/foo/", Rate: 5, Burst: 1},
	}
	r := newTestRLWithRules(10, rules)

	allowed, prefix := r.allowPath("1.1.1.1", "/v1/items/list")
	if prefix != "" {
		t.Errorf("expected no match, got prefix %q", prefix)
	}
	if !allowed {
		t.Error("non-matching path should pass through")
	}
}

func TestRealIP(t *testing.T) {
	tests := []struct {
		name         string
		remoteAddr   string
		xRealIP      string
		trustedProxy string
		want         string
	}{
		{name: "no proxy uses RemoteAddr", remoteAddr: "192.0.2.1:1234", trustedProxy: "", want: "192.0.2.1"},
		{name: "trusted proxy match accepts X-Real-IP",
			remoteAddr: "10.0.0.1:1234", xRealIP: "203.0.113.5", trustedProxy: "10.0.0.1",
			want: "203.0.113.5"},
		{name: "trusted proxy mismatch ignores X-Real-IP",
			remoteAddr: "192.0.2.1:1234", xRealIP: "203.0.113.5", trustedProxy: "10.0.0.1",
			want: "192.0.2.1"},
		{name: "trusted proxy match but invalid X-Real-IP falls back",
			remoteAddr: "10.0.0.1:1234", xRealIP: "not-an-ip", trustedProxy: "10.0.0.1",
			want: "10.0.0.1"},
		{name: "no port in RemoteAddr returned as-is",
			remoteAddr: "no-port", trustedProxy: "", want: "no-port"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := http.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.remoteAddr
			if tc.xRealIP != "" {
				r.Header.Set("X-Real-IP", tc.xRealIP)
			}
			got := realIP(r, tc.trustedProxy)
			if got != tc.want {
				t.Errorf("realIP() = %q, want %q", got, tc.want)
			}
		})
	}
}
