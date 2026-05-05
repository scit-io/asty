package asty

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// ServiceType defines how the service is scheduled
type ServiceType string

const (
	ServiceTypeSystem  ServiceType = "system"  // One copy per node
	ServiceTypeService ServiceType = "service" // Managed by autoscaler
)

// ServiceDefinition describes a service loaded from .asty file
type ServiceDefinition struct {
	Name         string      `yaml:"name"`
	Type         ServiceType `yaml:"type"`
	Artifact     Artifact    `yaml:"artifact"`
	Command      string      `yaml:"command"`
	User         string      `yaml:"user"`
	KillTimeout  string      `yaml:"kill_timeout"`
	Env          map[string]string `yaml:"env"`
	Resources    Resources   `yaml:"resources"`
	Health       Health      `yaml:"health"`
	Logs         Logs        `yaml:"logs"`
	Update       Update      `yaml:"update"`
	Restart      Restart     `yaml:"restart"`
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
	Type     string `yaml:"type"`     // http, tcp
	Path     string `yaml:"path"`     // for http
	Interval string `yaml:"interval"` // 10s
	Timeout  string `yaml:"timeout"`  // 3s
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

// LoadServiceDefinition loads a service definition from a .asty file
func LoadServiceDefinition(path string) (*ServiceDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read service file: %w", err)
	}

	var svc ServiceDefinition
	if err := yaml.Unmarshal(data, &svc); err != nil {
		return nil, fmt.Errorf("failed to parse service definition: %w", err)
	}

	// Validate required fields
	if svc.Name == "" {
		return nil, fmt.Errorf("service name is required")
	}
	if svc.Type != ServiceTypeSystem && svc.Type != ServiceTypeService {
		return nil, fmt.Errorf("invalid service type: %s", svc.Type)
	}

	return &svc, nil
}

// GetKillTimeout parses the kill_timeout field
func (s *ServiceDefinition) GetKillTimeout() time.Duration {
	d, _ := time.ParseDuration(s.KillTimeout)
	if d == 0 {
		return 30 * time.Second
	}
	return d
}

// GetHealthInterval parses the health check interval
func (h *Health) GetInterval() time.Duration {
	d, _ := time.ParseDuration(h.Interval)
	if d == 0 {
		return 10 * time.Second
	}
	return d
}

// GetHealthTimeout parses the health check timeout
func (h *Health) GetTimeout() time.Duration {
	d, _ := time.ParseDuration(h.Timeout)
	if d == 0 {
		return 3 * time.Second
	}
	return d
}

// GetRestartAttempts returns the number of restart attempts
func (r *Restart) GetAttempts() int {
	if r.Attempts <= 0 {
		return 3 // default: 3 attempts
	}
	return r.Attempts
}

// GetRestartDelay parses the restart delay
func (r *Restart) GetDelay() time.Duration {
	d, _ := time.ParseDuration(r.Delay)
	if d == 0 {
		return 5 * time.Second // default: 5 seconds
	}
	return d
}
