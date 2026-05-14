package health

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"asty/asty/internal/core/types"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// loopTickInterval — how often the checker wakes up to schedule probes.
// Each Check has its own Interval; the loop only fires probes whose
// per-check interval has elapsed since their last run, so tickInterval
// just bounds the worst-case scheduling lag.
const loopTickInterval = 1 * time.Second

// httpClientTimeout caps the agent-wide HTTP client. Per-probe timeouts
// from Health.GetTimeout() come on top via context, but this is the
// hard ceiling that prevents a slow process from monopolising the loop.
const httpClientTimeout = 3 * time.Second

// CheckType distinguishes between probe mechanisms.
type CheckType string

const (
	CheckTypeHTTP CheckType = "http"
	CheckTypeNATS CheckType = "nats"
)

// Check represents a single health check configuration and the most
// recent probe result.
type Check struct {
	ProcessName string
	Type        CheckType
	URL         string // HTTP: full URL; NATS: subject
	Interval    time.Duration
	Timeout     time.Duration
	Healthy     bool
	LastCheck   time.Time
	LastError   error
}

// Checker performs periodic health checks for registered processes.
type Checker struct {
	mu     sync.RWMutex
	checks map[string]*Check
	client *http.Client
	nc     *nats.Conn
}

// NewChecker creates a new health checker.
func NewChecker(nc *nats.Conn) *Checker {
	return &Checker{
		checks: make(map[string]*Check),
		client: &http.Client{Timeout: httpClientTimeout},
		nc:     nc,
	}
}

// Register adds (or replaces) an HTTP health check for a process.
// Mirrors RegisterNATS — no error return because nothing in the body
// can fail (the map write is in-process, the log line never errors).
func (c *Checker) Register(processName, addr, path string, interval, timeout time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	url := fmt.Sprintf("http://%s%s", addr, path)
	c.checks[processName] = &Check{
		ProcessName: processName,
		Type:        CheckTypeHTTP,
		URL:         url,
		Interval:    interval,
		Timeout:     timeout,
	}
	log.Info().
		Str("process", processName).
		Str("url", url).
		Dur("interval", interval).
		Msg("health check registered")
}

// RegisterNATS adds (or replaces) a NATS health check for a process.
func (c *Checker) RegisterNATS(processName, subject string, interval, timeout time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.checks[processName] = &Check{
		ProcessName: processName,
		Type:        CheckTypeNATS,
		URL:         subject,
		Interval:    interval,
		Timeout:     timeout,
	}
	log.Info().
		Str("process", processName).
		Str("subject", subject).
		Dur("interval", interval).
		Msg("health check registered (nats)")
}

// Unregister removes a process's health check.
func (c *Checker) Unregister(processName string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.checks, processName)
	log.Info().Str("process", processName).Msg("health check unregistered")
}

// Start runs the scheduling loop until ctx is cancelled.
func (c *Checker) Start(ctx context.Context) {
	ticker := time.NewTicker(loopTickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.runChecks(ctx)
		}
	}
}

// IsHealthy reports the most recent probe result for processName.
// Returns false for unregistered processes.
func (c *Checker) IsHealthy(processName string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	check, ok := c.checks[processName]
	return ok && check.Healthy
}

// HealthStatus returns the latest probe state for processName, or
// HealthUnknown if no probe is registered. Used by the agent to
// populate Allocation.HealthStatus.
func (c *Checker) HealthStatus(processName string) types.HealthState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	check, ok := c.checks[processName]
	if !ok {
		return types.HealthUnknown
	}
	if check.Healthy {
		return types.HealthHealthy
	}
	return types.HealthUnhealthy
}
