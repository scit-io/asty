package asty

import "asty/internal/platform/asty/features/deployment"

// Backward-compatible aliases
type ServiceLoader = deployment.ServiceLoader

var NewServiceLoader = deployment.NewServiceLoader
