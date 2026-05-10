package types

import "time"

// ServiceType defines how the service is scheduled
type ServiceType string

const (
	ServiceTypeSystem  ServiceType = "system"
	ServiceTypeService ServiceType = "service"
)

// ServiceDefinition describes a service loaded from .asty file
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
	Logs        Logs              `yaml:"logs"`
	Update      Update            `yaml:"update"`
	Restart     Restart           `yaml:"restart"`
}

type Artifact struct {
	URL      string `yaml:"url"`
	Checksum string `yaml:"checksum"`
}

type Resources struct {
	CPU    int `yaml:"cpu"`    // MHz
	Memory int `yaml:"memory"` // MB
}

type Health struct {
	Type     string `yaml:"type"`
	Addr     string `yaml:"addr"`
	Path     string `yaml:"path"`
	Interval string `yaml:"interval"`
	Timeout  string `yaml:"timeout"`
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

type Restart struct {
	Attempts int    `yaml:"attempts"`
	Interval string `yaml:"interval"`
	Delay    string `yaml:"delay"`
}

// GetKillTimeout parses the kill_timeout field
func (s *ServiceDefinition) GetKillTimeout() time.Duration {
	d, _ := time.ParseDuration(s.KillTimeout)
	if d == 0 {
		return 30 * time.Second
	}
	return d
}

// GetInterval parses the health check interval
func (h *Health) GetInterval() time.Duration {
	d, _ := time.ParseDuration(h.Interval)
	if d == 0 {
		return 10 * time.Second
	}
	return d
}

// GetTimeout parses the health check timeout
func (h *Health) GetTimeout() time.Duration {
	d, _ := time.ParseDuration(h.Timeout)
	if d == 0 {
		return 3 * time.Second
	}
	return d
}

// GetAttempts returns the number of restart attempts
func (r *Restart) GetAttempts() int {
	if r.Attempts <= 0 {
		return 3
	}
	return r.Attempts
}

// GetDelay parses the restart delay
func (r *Restart) GetDelay() time.Duration {
	d, _ := time.ParseDuration(r.Delay)
	if d == 0 {
		return 5 * time.Second
	}
	return d
}
