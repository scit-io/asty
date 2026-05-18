package types

import "time"

// ParseDurationOr parses a duration string and returns fallback for
// empty input, a parse error, or a zero result. The d == 0 fallback
// matters for fields where literal "0s" is semantically dangerous
// (e.g. kill_timeout=0 means SIGKILL without grace; update.*=0 means
// "skip the wait", breaking rolling updates).
func ParseDurationOr(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil || d == 0 {
		return fallback
	}
	return d
}

// GetKillTimeout returns the parsed kill_timeout, with default fallback.
func (s *ServiceDefinition) GetKillTimeout() time.Duration {
	if s.parsedKillTimeout != 0 {
		return s.parsedKillTimeout
	}
	return ParseDurationOr(s.KillTimeout, defaultKillTimeout)
}

// GetInterval returns the parsed health.interval, with default fallback.
func (h *Health) GetInterval() time.Duration {
	if h.parsedInterval != 0 {
		return h.parsedInterval
	}
	return ParseDurationOr(h.Interval, defaultHealthInterval)
}

// GetTimeout returns the parsed health.timeout, with default fallback.
func (h *Health) GetTimeout() time.Duration {
	if h.parsedTimeout != 0 {
		return h.parsedTimeout
	}
	return ParseDurationOr(h.Timeout, defaultHealthTimeout)
}

// GetAttempts returns the configured restart attempt count or the
// default when zero. No parsing needed — it's already an int.
func (r *Restart) GetAttempts() int {
	if r.Attempts <= 0 {
		return defaultRestartTries
	}
	return r.Attempts
}

// GetDelay returns the parsed restart.delay, with default fallback.
func (r *Restart) GetDelay() time.Duration {
	if r.parsedDelay != 0 {
		return r.parsedDelay
	}
	return ParseDurationOr(r.Delay, defaultRestartDelay)
}
