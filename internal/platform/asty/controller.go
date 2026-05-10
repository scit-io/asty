package asty

import "asty/internal/platform/asty/features/clustering/controller"

// Backward-compatible aliases
type ServiceController = controller.ServiceController
type CommandDispatcher = controller.CommandDispatcher

var NewServiceController = controller.NewServiceController
