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
	defaultMaxParallel    = 1
	defaultCanary         = 1
	defaultCanaryRetries  = 1
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
	CPU    int `yaml:"cpu"`              // MHz
	Memory int `yaml:"memory"`           // MB
	Disk   int `yaml:"disk,omitempty"`   // MB — optional. When >0 the UI renders per-alloc disk as a percentage of this budget, matching the CPU/RAM cells; when 0 only the raw usage is shown.
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
	PathPrefix string  `yaml:"path_prefix"` // e.g. "/<gateway-prefix>/<service>/"
	Rate       float64 `yaml:"rate"`        // Requests per second per IP
	Burst      int     `yaml:"burst"`       // Burst above steady rate
}

type Logs struct {
	MaxFiles    int `yaml:"max_files"`
	MaxFileSize int `yaml:"max_file_size"` // MB
}

// Update governs rolling-update behaviour. MaxParallel and Canary
// default to 1 in Resolve() — a zero MaxParallel would deadlock the
// rolling loop, and a zero Canary disables the canary phase.
//
// CanaryRetries (default 1) is the number of times the deployer
// re-dispatches an unhealthy canary before failing/reverting. Useful
// when the artifact pulls slowly on a cold cache: the first attempt
// might just miss the HealthyDeadline, while the second arrives with
// the binary already on disk.
type Update struct {
	MaxParallel     int    `yaml:"max_parallel"`
	Canary          int    `yaml:"canary"`
	CanaryRetries   int    `yaml:"canary_retries"`
	MinHealthyTime  string `yaml:"min_healthy_time"`
	HealthyDeadline string `yaml:"healthy_deadline"`
	AutoRevert      bool   `yaml:"auto_revert"`
}

// GetMaxParallel returns the per-batch parallelism for rolling updates,
// substituting the default when the .asty leaves it ≤ 0. The deployer's
// rolling loop divides by this value, so 0 is a deadlock — Resolve()
// also normalises the field, this method is the safety net.
func (u Update) GetMaxParallel() int {
	if u.MaxParallel > 0 {
		return u.MaxParallel
	}
	return defaultMaxParallel
}

// GetCanary returns the canary copy count, substituting the default
// when the .asty leaves it ≤ 0. A negative value would be parsed back
// to 0 by clamping in deployment.deployCanary.
func (u Update) GetCanary() int {
	if u.Canary > 0 {
		return u.Canary
	}
	return defaultCanary
}

// Restart describes the per-service restart policy plus the
// pre-parsed delay duration. The failure counter (ConsecutiveFailures
// on the allocation) resets on the next successful start, so Attempts
// caps consecutive failures, not failures within a time window.
type Restart struct {
	Attempts int    `yaml:"attempts"`
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
//
// Also normalises Update.MaxParallel and Update.Canary in place to
// avoid the deadlock-on-zero-MaxParallel trap in the rolling deployer.
func (s *ServiceDefinition) Resolve() {
	s.parsedKillTimeout = ParseDurationOr(s.KillTimeout, defaultKillTimeout)
	s.Health.parsedInterval = ParseDurationOr(s.Health.Interval, defaultHealthInterval)
	s.Health.parsedTimeout = ParseDurationOr(s.Health.Timeout, defaultHealthTimeout)
	s.Restart.parsedDelay = ParseDurationOr(s.Restart.Delay, defaultRestartDelay)
	if s.Update.MaxParallel <= 0 {
		s.Update.MaxParallel = defaultMaxParallel
	}
	if s.Update.Canary < 0 {
		s.Update.Canary = 0
	}
	if s.Update.CanaryRetries < 0 {
		s.Update.CanaryRetries = 0
	}
}

// Per-field accessors with default fallback live in
// service_accessors.go alongside ParseDurationOr.
