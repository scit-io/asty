package asty

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// MetricsCollector collects CPU and Memory metrics for processes
type MetricsCollector struct {
	mu sync.RWMutex

	metrics  map[int]*ProcessMetrics // key: PID
	interval time.Duration
}

// ProcessMetrics holds resource usage metrics for a process
type ProcessMetrics struct {
	PID          int
	ProcessName  string
	CPUPercent   float64 // 0-100 per core
	MemoryMB     int64   // RSS in MB
	LastUpdated  time.Time
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(interval time.Duration) *MetricsCollector {
	return &MetricsCollector{
		metrics:  make(map[int]*ProcessMetrics),
		interval: interval,
	}
}

// Register registers a process for metrics collection
func (mc *MetricsCollector) Register(pid int, processName string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.metrics[pid] = &ProcessMetrics{
		PID:         pid,
		ProcessName: processName,
		LastUpdated: time.Time{},
	}

	log.Info().
		Int("pid", pid).
		Str("process", processName).
		Msg("metrics collector: registered process")
}

// Unregister removes a process from metrics collection
func (mc *MetricsCollector) Unregister(pid int) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	delete(mc.metrics, pid)

	log.Info().
		Int("pid", pid).
		Msg("metrics collector: unregistered process")
}

// Start starts the metrics collection loop
func (mc *MetricsCollector) Start(ctx context.Context) {
	ticker := time.NewTicker(mc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			mc.collectMetrics()
		}
	}
}

// collectMetrics collects metrics for all registered processes
func (mc *MetricsCollector) collectMetrics() {
	mc.mu.RLock()
	pids := make([]int, 0, len(mc.metrics))
	for pid := range mc.metrics {
		pids = append(pids, pid)
	}
	mc.mu.RUnlock()

	for _, pid := range pids {
		mc.collectProcessMetrics(pid)
	}
}

// collectProcessMetrics collects metrics for a single process
func (mc *MetricsCollector) collectProcessMetrics(pid int) {
	// Read /proc/[pid]/stat for CPU
	cpuPercent, err := mc.getCPUPercent(pid)
	if err != nil {
		// Process may have exited
		return
	}

	// Read /proc/[pid]/status for Memory
	memoryMB, err := mc.getMemoryMB(pid)
	if err != nil {
		return
	}

	mc.mu.Lock()
	defer mc.mu.Unlock()

	if metrics, exists := mc.metrics[pid]; exists {
		metrics.CPUPercent = cpuPercent
		metrics.MemoryMB = memoryMB
		metrics.LastUpdated = time.Now()
	}
}

// GetMetrics returns current metrics for a process
func (mc *MetricsCollector) GetMetrics(pid int) (*ProcessMetrics, bool) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	metrics, exists := mc.metrics[pid]
	if !exists {
		return nil, false
	}

	// Return a copy
	return &ProcessMetrics{
		PID:         metrics.PID,
		ProcessName: metrics.ProcessName,
		CPUPercent:  metrics.CPUPercent,
		MemoryMB:    metrics.MemoryMB,
		LastUpdated: metrics.LastUpdated,
	}, true
}

// GetAllMetrics returns metrics for all processes
func (mc *MetricsCollector) GetAllMetrics() map[int]*ProcessMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	result := make(map[int]*ProcessMetrics, len(mc.metrics))
	for pid, metrics := range mc.metrics {
		result[pid] = &ProcessMetrics{
			PID:         metrics.PID,
			ProcessName: metrics.ProcessName,
			CPUPercent:  metrics.CPUPercent,
			MemoryMB:    metrics.MemoryMB,
			LastUpdated: metrics.LastUpdated,
		}
	}

	return result
}

// getCPUPercent reads CPU usage from /proc/[pid]/stat
// Returns percentage 0-100 per core
func (mc *MetricsCollector) getCPUPercent(pid int) (float64, error) {
	// This is a simplified implementation
	// TODO: implement proper CPU percentage calculation with delta time
	// For now, just return 0 as placeholder

	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := os.ReadFile(statPath)
	if err != nil {
		return 0, err
	}

	// Parse stat file (simplified)
	// Format: pid (name) state utime stime ...
	// We need utime + stime for CPU time
	fields := strings.Fields(string(data))
	if len(fields) < 15 {
		return 0, fmt.Errorf("invalid stat format")
	}

	// For now, return 0 - proper implementation requires tracking deltas
	// TODO: track previous values and calculate percentage
	return 0, nil
}

// getMemoryMB reads memory usage from /proc/[pid]/status
// Returns RSS (Resident Set Size) in MB
func (mc *MetricsCollector) getMemoryMB(pid int) (int64, error) {
	statusPath := fmt.Sprintf("/proc/%d/status", pid)
	data, err := os.ReadFile(statusPath)
	if err != nil {
		return 0, err
	}

	// Find VmRSS line
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.ParseInt(fields[1], 10, 64)
				if err != nil {
					return 0, err
				}
				return kb / 1024, nil // Convert KB to MB
			}
		}
	}

	return 0, fmt.Errorf("VmRSS not found")
}
