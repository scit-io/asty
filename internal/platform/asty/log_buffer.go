package asty

import "asty/internal/platform/asty/features/observability/logs"

// Backward-compatible aliases
type LogLine = logs.LogLine
type LogBuffer = logs.Buffer

var NewLogBuffer = logs.NewBuffer
