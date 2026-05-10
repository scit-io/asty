package health

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Check represents a single health check configuration
type Check struct {
	ProcessName string
	URL         string
	Interval    time.Duration
	Timeout     time.Duration
	Healthy     bool
	LastCheck   time.Time
	LastError   error
}

// Checker performs periodic health checks on processes
type Checker struct {
	mu sync.RWMutex

	checks map[string]*Check
	client *http.Client
}

// NewChecker creates a new health checker
func NewChecker() *Checker {
	return &Checker{
		checks: make(map[string]*Check),
		client: &http.Client{
			Timeout: 3 * time.Second,
		},
	}
}

// Register registers a health check for a process
func (c *Checker) Register(processName, addr, path string, interval, timeout time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	url := fmt.Sprintf("http://%s%s", addr, path)

	check := &Check{
		ProcessName: processName,
		URL:         url,
		Interval:    interval,
		Timeout:     timeout,
		Healthy:     false,
		LastCheck:   time.Time{},
	}

	c.checks[processName] = check

	log.Info().
		Str("process", processName).
		Str("url", url).
		Dur("interval", interval).
		Msg("health check registered")

	return nil
}

// Unregister removes a health check
func (c *Checker) Unregister(processName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.checks, processName)

	log.Info().
		Str("process", processName).
		Msg("health check unregistered")
}

// Start starts the health checker loop
func (c *Checker) Start(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
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

func (c *Checker) runChecks(ctx context.Context) {
	c.mu.RLock()
	checks := make([]*Check, 0, len(c.checks))
	for _, check := range c.checks {
		if time.Since(check.LastCheck) >= check.Interval {
			checks = append(checks, check)
		}
	}
	c.mu.RUnlock()

	for _, check := range checks {
		go c.performCheck(ctx, check)
	}
}

func (c *Checker) performCheck(ctx context.Context, check *Check) {
	checkCtx, cancel := context.WithTimeout(ctx, check.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(checkCtx, "GET", check.URL, nil)
	if err != nil {
		c.recordResult(check, false, err)
		return
	}

	resp, err := c.client.Do(req)
	if err != nil {
		c.recordResult(check, false, err)
		return
	}
	defer resp.Body.Close()

	healthy := resp.StatusCode >= 200 && resp.StatusCode < 300
	c.recordResult(check, healthy, nil)
}

func (c *Checker) recordResult(check *Check, healthy bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	check.LastCheck = time.Now()
	check.LastError = err

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
func (c *Checker) IsHealthy(processName string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	check, exists := c.checks[processName]
	if !exists {
		return false
	}

	return check.Healthy
}

// HealthStatusStr returns "healthy", "unhealthy", or "" (no probe registered).
func (c *Checker) HealthStatusStr(processName string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	check, exists := c.checks[processName]
	if !exists {
		return ""
	}
	if check.Healthy {
		return "healthy"
	}
	return "unhealthy"
}

// GetStatus returns the health check status for a process
func (c *Checker) GetStatus(processName string) (*Check, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	check, exists := c.checks[processName]
	if !exists {
		return nil, false
	}

	return &Check{
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
func (c *Checker) GetAllStatuses() map[string]*Check {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]*Check, len(c.checks))
	for name, check := range c.checks {
		result[name] = &Check{
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
