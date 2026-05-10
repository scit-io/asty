package asty

import (
	"fmt"
	"os"

	"asty/internal/platform/asty/core/types"

	"gopkg.in/yaml.v3"
)

// Type aliases for backward compatibility
type ServiceType = types.ServiceType
type ServiceDefinition = types.ServiceDefinition
type Artifact = types.Artifact
type Resources = types.Resources
type Health = types.Health
type Logs = types.Logs
type Update = types.Update
type Restart = types.Restart

const (
	ServiceTypeSystem  = types.ServiceTypeSystem
	ServiceTypeService = types.ServiceTypeService
)

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

	if svc.Name == "" {
		return nil, fmt.Errorf("service name is required")
	}
	if svc.Type != ServiceTypeSystem && svc.Type != ServiceTypeService {
		return nil, fmt.Errorf("invalid service type: %s", svc.Type)
	}

	return &svc, nil
}
