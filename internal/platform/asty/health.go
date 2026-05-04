package asty

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// HealthChecker performs periodic health checks on processes
type HealthChecker struct {
	mu sync.RWMutex

	checks   map[string]*HealthCheck
	interval time.Duration
	client   *http.Client
}

// HealthCheck represents a single health check configuration
type HealthCheck struct {
	ProcessName string
	URL         string
	Interval    time.Duration
	Timeout     time.Duration
	Healthy     bool
	LastCheck   time.Time
	LastError   error
}

// NewHealthChecker creates a new health checker
func NewHealthChecker() *HealthChecker {
	return &HealthChecker{
		checks: make(map[string]*HealthCheck),
		client: &http.Client{
			Timeout: 3 * time.Second,
		},
	}
}

// Register registers a health check for a process
func (hc *HealthChecker) Register(processName, addr, path string, interval, timeout time.Duration) error {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	url := fmt.Sprintf("http://%s%s", addr, path)

	check := &HealthCheck{
		ProcessName: processName,
		URL:         url,
		Interval:    interval,
		Timeout:     timeout,
		Healthy:     false,
		LastCheck:   time.Time{},
	}

	hc.checks[processName] = check

	log.Info().
		Str("process", processName).
		Str("url", url).
		Dur("interval", interval).
		Msg("health check registered")

	return nil
}

// Unregister removes a health check
func (hc *HealthChecker) Unregister(processName string) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	delete(hc.checks, processName)

	log.Info().
		Str("process", processName).
		Msg("health check unregistered")
}

// Start starts the health checker loop
func (hc *HealthChecker) Start(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second) // Check every second for intervals
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hc.runChecks(ctx)
		}
	}
}

// runChecks executes all registered health checks that are due
func (hc *HealthChecker) runChecks(ctx context.Context) {
	hc.mu.RLock()
	checks := make([]*HealthCheck, 0, len(hc.checks))
	for _, check := range hc.checks {
		if time.Since(check.LastCheck) >= check.Interval {
			checks = append(checks, check)
		}
	}
	hc.mu.RUnlock()

	// Run checks concurrently
	for _, check := range checks {
		go hc.performCheck(ctx, check)
	}
}

// performCheck executes a single health check
func (hc *HealthChecker) performCheck(ctx context.Context, check *HealthCheck) {
	checkCtx, cancel := context.WithTimeout(ctx, check.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(checkCtx, "GET", check.URL, nil)
	if err != nil {
		hc.recordResult(check, false, err)
		return
	}

	resp, err := hc.client.Do(req)
	if err != nil {
		hc.recordResult(check, false, err)
		return
	}
	defer resp.Body.Close()

	healthy := resp.StatusCode >= 200 && resp.StatusCode < 300
	hc.recordResult(check, healthy, nil)
}

// recordResult records the result of a health check
func (hc *HealthChecker) recordResult(check *HealthCheck, healthy bool, err error) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	check.LastCheck = time.Now()
	check.LastError = err

	// State change logging
	if check.Healthy != healthy {
		if healthy {
			log.Info().
				Str("process", check.ProcessName).
				Str("url", check.URL).
				Msg("health check: now healthy")
		} else {
			log.Warn().
				Err(err).
				Str("process", check.ProcessName).
				Str("url", check.URL).
				Msg("health check: now unhealthy")
		}
	}

	check.Healthy = healthy
}

// IsHealthy returns whether a process is currently healthy
func (hc *HealthChecker) IsHealthy(processName string) bool {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	check, exists := hc.checks[processName]
	if !exists {
		return false
	}

	return check.Healthy
}

// GetStatus returns the health check status for a process
func (hc *HealthChecker) GetStatus(processName string) (*HealthCheck, bool) {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	check, exists := hc.checks[processName]
	if !exists {
		return nil, false
	}

	// Return a copy to avoid race conditions
	return &HealthCheck{
		ProcessName: check.ProcessName,
		URL:         check.URL,
		Interval:    check.Interval,
		Timeout:     check.Timeout,
		Healthy:     check.Healthy,
		LastCheck:   check.LastCheck,
		LastError:   check.LastError,
	}, true
}

// GetAllStatuses returns health check statuses for all processes
func (hc *HealthChecker) GetAllStatuses() map[string]*HealthCheck {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	result := make(map[string]*HealthCheck, len(hc.checks))
	for name, check := range hc.checks {
		result[name] = &HealthCheck{
			ProcessName: check.ProcessName,
			URL:         check.URL,
			Interval:    check.Interval,
			Timeout:     check.Timeout,
			Healthy:     check.Healthy,
			LastCheck:   check.LastCheck,
			LastError:   check.LastError,
		}
	}

	return result
}
