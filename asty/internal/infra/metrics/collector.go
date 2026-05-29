package metrics

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// ProcessMetrics holds resource usage metrics for a process
type ProcessMetrics struct {
	PID         int
	ProcessName string
	CPUPercent  float64
	MemoryMB    int64
	LastUpdated time.Time
}

// Collector collects CPU and Memory metrics for processes
type Collector struct {
	mu sync.RWMutex

	metrics   map[int]*ProcessMetrics
	prevTicks map[int]uint64
	interval  time.Duration
}

// NewCollector creates a new metrics collector
func NewCollector(interval time.Duration) *Collector {
	return &Collector{
		metrics:   make(map[int]*ProcessMetrics),
		prevTicks: make(map[int]uint64),
		interval:  interval,
	}
}

// Register registers a process for metrics collection
func (c *Collector) Register(pid int, processName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.metrics[pid] = &ProcessMetrics{
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
func (c *Collector) Unregister(pid int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.metrics, pid)
	delete(c.prevTicks, pid)

	log.Info().
		Int("pid", pid).
		Msg("metrics collector: unregistered process")
}

// Start starts the metrics collection loop
func (c *Collector) Start(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collectMetrics()
		}
	}
}

func (c *Collector) collectMetrics() {
	c.mu.RLock()
	pids := make([]int, 0, len(c.metrics))
	for pid := range c.metrics {
		pids = append(pids, pid)
	}
	c.mu.RUnlock()

	for _, pid := range pids {
		c.collectProcessMetrics(pid)
	}
}

func (c *Collector) collectProcessMetrics(pid int) {
	cpuPercent, err := c.getCPUPercent(pid)
	if err != nil {
		return
	}

	memoryMB, err := c.getMemoryMB(pid)
	if err != nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if metrics, exists := c.metrics[pid]; exists {
		metrics.CPUPercent = cpuPercent
		metrics.MemoryMB = memoryMB
		metrics.LastUpdated = time.Now()
	}
}

// GetMetrics returns current metrics for a process
func (c *Collector) GetMetrics(pid int) (*ProcessMetrics, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	metrics, exists := c.metrics[pid]
	if !exists {
		return nil, false
	}

	return &ProcessMetrics{
		PID:         metrics.PID,
		ProcessName: metrics.ProcessName,
		CPUPercent:  metrics.CPUPercent,
		MemoryMB:    metrics.MemoryMB,
		LastUpdated: metrics.LastUpdated,
	}, true
}
