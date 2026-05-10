package asty

import (
	"asty/internal/platform/asty/core/types"
	"asty/internal/platform/asty/features/deployment"
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
var LoadServiceDefinition = deployment.LoadServiceDefinition
