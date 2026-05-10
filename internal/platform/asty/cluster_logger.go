package asty

import "asty/internal/platform/asty/features/observability/logs"

// Backward-compatible aliases
type NATSWriter = logs.NATSWriter

var NewNATSWriter = logs.NewNATSWriter
