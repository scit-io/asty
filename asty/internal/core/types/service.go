package types

import "time"

// Default time-window fallbacks used when a .asty file omits the
// corresponding field. Promoted to constants so callers can refer to
// them when comparing intervals.
const (
	defaultKillTimeout    = 30 * time.Second
	defaultHealthInterval = 10 * time.Second
	defaultHealthTimeout  = 3 * time.Second
	defaultRestartDelay   = 5 * time.Second
	defaultRestartTries   = 3
)

// ServiceType defines how the service is scheduled.
type ServiceType string

const (
	ServiceTypeSystem  ServiceType = "system"
	ServiceTypeService ServiceType = "service"
)

// ServiceDefinition describes a service loaded from a .asty file.
//
// Duration-typed fields on the file are stored as raw strings (matching
// the YAML spelling like "10s") plus a pre-parsed time.Duration in the
// unexported `parsed*` fields. The loader calls Resolve() after
// unmarshal so the parsed values are ready when the rest of the system
// reads them via the Get* methods.
type ServiceDefinition struct {
	Name        string            `yaml:"name"`
	Type        ServiceType       `yaml:"type"`
	Artifact    Artifact          `yaml:"artifact"`
	Command     string            `yaml:"command"`
	User        string            `yaml:"user"`
	KillTimeout string            `yaml:"kill_timeout"`
	Env         map[string]string `yaml:"env"`
	Resources   Resources         `yaml:"resources"`
	Health      Health            `yaml:"health"`
	RateLimit   []RateLimitRule   `yaml:"rate_limit"`
	Logs        Logs              `yaml:"logs"`
	Update      Update            `yaml:"update"`
	Restart     Restart           `yaml:"restart"`
	KV          []KVBucket        `yaml:"kv"`

	// parsedKillTimeout is set by Resolve(); 0 falls back to default
	// in GetKillTimeout. Excluded from YAML/JSON so disk format stays
	// unchanged.
	parsedKillTimeout time.Duration `yaml:"-" json:"-"`
}

type Artifact struct {
	URL      string `yaml:"url"`
	Checksum string `yaml:"checksum"`
}

type Resources struct {
	CPU    int `yaml:"cpu"`    // MHz
	Memory int `yaml:"memory"` // MB
}

// Health holds the health-probe configuration plus pre-parsed durations.
// Supported types: "http" (GET addr+path) and "nats" (request to subject).
type Health struct {
	Type    string `yaml:"type"`
	Addr    string `yaml:"addr"`
	Path    string `yaml:"path"`
	Subject string `yaml:"subject"`

	Interval string `yaml:"interval"`
	Timeout  string `yaml:"timeout"`

	parsedInterval time.Duration `yaml:"-" json:"-"`
	parsedTimeout  time.Duration `yaml:"-" json:"-"`
}

// RateLimitRule declares a per-path rate limit enforced by the gateway.
// Services define their own rules — the gateway collects them at startup
// and applies the matching rule (longest prefix wins) before proxying.
type RateLimitRule struct {
	PathPrefix string  `yaml:"path_prefix"` // e.g. "/v1/auth/"
	Rate       float64 `yaml:"rate"`        // Requests per second per IP
	Burst      int     `yaml:"burst"`       // Burst above steady rate
}

type Logs struct {
	MaxFiles    int `yaml:"max_files"`
	MaxFileSize int `yaml:"max_file_size"` // MB
}

type Update struct {
	MaxParallel      int    `yaml:"max_parallel"`
	MinHealthyTime   string `yaml:"min_healthy_time"`
	HealthyDeadline  string `yaml:"healthy_deadline"`
	ProgressDeadline string `yaml:"progress_deadline"`
	AutoRevert       bool   `yaml:"auto_revert"`
}

// Restart describes the per-service restart policy plus the
// pre-parsed delay duration.
type Restart struct {
	Attempts int    `yaml:"attempts"`
	Interval string `yaml:"interval"`
	Delay    string `yaml:"delay"`

	parsedDelay time.Duration `yaml:"-" json:"-"`
}

// KVBucket declares a NATS JetStream KV bucket that must exist before
// the service starts. The server creates it at deploy time.
type KVBucket struct {
	Bucket   string `yaml:"bucket"`
	History  int    `yaml:"history"`
	TTL      string `yaml:"ttl"`
	Replicas int    `yaml:"replicas"`
}

// Resolve parses every time-typed string field once and caches the
// result. Callers (notably the loader) must invoke it after unmarshal;
// the Get* methods fall back to default values if Resolve was skipped,
// so behaviour is correct either way — just less efficient.
func (s *ServiceDefinition) Resolve() {
	s.parsedKillTimeout = ParseDurationOr(s.KillTimeout, defaultKillTimeout)
	s.Health.parsedInterval = ParseDurationOr(s.Health.Interval, defaultHealthInterval)
	s.Health.parsedTimeout = ParseDurationOr(s.Health.Timeout, defaultHealthTimeout)
	s.Restart.parsedDelay = ParseDurationOr(s.Restart.Delay, defaultRestartDelay)
}

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
