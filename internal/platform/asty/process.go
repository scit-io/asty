package asty

import "asty/internal/platform/asty/features/execution/process"

// Backward-compatible aliases
type Process = process.Process
type ProcessStatus = process.Status

const (
	ProcessStatusStarting = process.StatusStarting
	ProcessStatusRunning  = process.StatusRunning
	ProcessStatusStopping = process.StatusStopping
	ProcessStatusStopped  = process.StatusStopped
	ProcessStatusFailed   = process.StatusFailed
)

var NewProcess = process.New
