package deployment

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

// ServiceLoader loads service definitions from directory
type ServiceLoader struct {
	directory string
}

// NewServiceLoader creates a new service loader
func NewServiceLoader(directory string) *ServiceLoader {
	return &ServiceLoader{
		directory: directory,
	}
}

// LoadAll loads all .asty files from directory
func (sl *ServiceLoader) LoadAll() ([]*types.ServiceDefinition, error) {
	if sl.directory == "" {
		return nil, fmt.Errorf("service directory not specified")
	}

	if _, err := os.Stat(sl.directory); os.IsNotExist(err) {
		log.Warn().Str("directory", sl.directory).Msg("service directory does not exist")
		return []*types.ServiceDefinition{}, nil
	}

	entries, err := os.ReadDir(sl.directory)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	services := make([]*types.ServiceDefinition, 0)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if !strings.HasSuffix(entry.Name(), ".asty") {
			continue
		}

		path := filepath.Join(sl.directory, entry.Name())
		svc, err := LoadServiceDefinition(path)
		if err != nil {
			log.Error().Err(err).Str("file", entry.Name()).Msg("failed to load service definition")
			continue
		}

		services = append(services, svc)
		log.Info().Str("service", svc.Name).Str("type", string(svc.Type)).Msg("loaded service definition")
	}

	log.Info().Int("count", len(services)).Msg("loaded service definitions")

	return services, nil
}

// Watch watches directory for changes
func (sl *ServiceLoader) Watch(onChange func([]*types.ServiceDefinition)) {
	services, err := sl.LoadAll()
	if err != nil {
		log.Error().Err(err).Msg("failed to load services")
		return
	}

	onChange(services)
}

// GetService loads a specific service by name
func (sl *ServiceLoader) GetService(name string) (*types.ServiceDefinition, error) {
	path := filepath.Join(sl.directory, name+".asty")
	return LoadServiceDefinition(path)
}

// LoadServiceDefinition loads a service definition from a .asty file
func LoadServiceDefinition(path string) (*types.ServiceDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read service file: %w", err)
	}

	var svc types.ServiceDefinition
	if err := yaml.Unmarshal(data, &svc); err != nil {
		return nil, fmt.Errorf("failed to parse service definition: %w", err)
	}

	if svc.Name == "" {
		return nil, fmt.Errorf("service name is required")
	}
	if svc.Type != types.ServiceTypeSystem && svc.Type != types.ServiceTypeService {
		return nil, fmt.Errorf("invalid service type: %s", svc.Type)
	}

	// Cache parsed durations up front so the hot path (heartbeat,
	// metrics, health checks) doesn't re-parse strings every call.
	svc.Resolve()
	return &svc, nil
}
