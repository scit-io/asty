package probe

import (
	"context"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// runChecks dispatches one probe per check whose Interval has elapsed.
// Each probe runs in its own goroutine so a slow endpoint can't block
// the others.
func (c *Checker) runChecks(ctx context.Context) {
	c.mu.RLock()
	due := make([]*Check, 0, len(c.checks))
	for _, check := range c.checks {
		if time.Since(check.LastCheck) >= check.Interval {
			due = append(due, check)
		}
	}
	c.mu.RUnlock()

	for _, check := range due {
		go c.performCheck(ctx, check)
	}
}

func (c *Checker) performCheck(ctx context.Context, check *Check) {
	switch check.Type {
	case CheckTypeNATS:
		c.performNATSCheck(check)
	default:
		c.performHTTPCheck(ctx, check)
	}
}

func (c *Checker) performHTTPCheck(ctx context.Context, check *Check) {
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

	c.recordResult(check, resp.StatusCode >= 200 && resp.StatusCode < 300, nil)
}

func (c *Checker) performNATSCheck(check *Check) {
	if c.nc == nil {
		c.recordResult(check, false, nil)
		return
	}
	_, err := c.nc.Request(check.URL, nil, check.Timeout)
	c.recordResult(check, err == nil, err)
}

func (c *Checker) recordResult(check *Check, healthy bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	check.LastCheck = time.Now()
	check.LastError = err

	if check.Healthy != healthy {
		evt := log.Info()
		state := "now healthy"
		if !healthy {
			evt = log.Warn().Err(err)
			state = "now unhealthy"
		}
		evt.Str("process", check.ProcessName).Str("url", check.URL).Msg("health check: " + state)
	}
	check.Healthy = healthy
}

// cloneCheck returns a defensive copy so callers can't mutate the
// checker's internal state by holding onto a returned pointer.
func cloneCheck(c *Check) *Check {
	cp := *c
	return &cp
}

// GetStatus returns a snapshot of one check by process name.
func (c *Checker) GetStatus(processName string) (*Check, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	check, ok := c.checks[processName]
	if !ok {
		return nil, false
	}
	return cloneCheck(check), true
}

// GetAllStatuses returns snapshots of every registered check.
func (c *Checker) GetAllStatuses() map[string]*Check {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]*Check, len(c.checks))
	for name, check := range c.checks {
		out[name] = cloneCheck(check)
	}
	return out
}
