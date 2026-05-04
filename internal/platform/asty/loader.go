package asty

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
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
func (sl *ServiceLoader) LoadAll() ([]*ServiceDefinition, error) {
	if sl.directory == "" {
		return nil, fmt.Errorf("service directory not specified")
	}

	// Check if directory exists
	if _, err := os.Stat(sl.directory); os.IsNotExist(err) {
		log.Warn().Str("directory", sl.directory).Msg("service directory does not exist")
		return []*ServiceDefinition{}, nil
	}

	// Read directory
	entries, err := os.ReadDir(sl.directory)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	services := make([]*ServiceDefinition, 0)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Only load .asty files
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
func (sl *ServiceLoader) Watch(onChange func([]*ServiceDefinition)) {
	// TODO: implement file watching with fsnotify
	// For now, just load once
	services, err := sl.LoadAll()
	if err != nil {
		log.Error().Err(err).Msg("failed to load services")
		return
	}

	onChange(services)
}

// GetService loads a specific service by name
func (sl *ServiceLoader) GetService(name string) (*ServiceDefinition, error) {
	path := filepath.Join(sl.directory, name+".asty")
	return LoadServiceDefinition(path)
}
