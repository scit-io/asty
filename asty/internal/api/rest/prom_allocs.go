package rest

import (
	"time"

	"asty/asty/internal/core/types"

	"github.com/prometheus/client_golang/prometheus"
)

// allocCollector emits per-allocation gauges (`asty_alloc_*`). Like
// nodeCollector it pulls a fresh snapshot per scrape so allocations
// that disappear stop being reported automatically.
type allocCollector struct {
	api *API

	cpuPercent    *prometheus.Desc
	memoryMB      *prometheus.Desc
	diskMB        *prometheus.Desc
	restartsTotal *prometheus.Desc
	uptimeSeconds *prometheus.Desc
	health        *prometheus.Desc
	status        *prometheus.Desc
}

func newAllocCollector(api *API) *allocCollector {
	common := []string{"service", "node_id", "alloc_id"}
	healthLabels := []string{"service", "node_id", "alloc_id", "state"}
	statusLabels := []string{"service", "node_id", "alloc_id", "status"}
	return &allocCollector{
		api: api,
		cpuPercent: prometheus.NewDesc("asty_alloc_cpu_percent",
			"CPU% consumed by the allocation's process.", common, nil),
		memoryMB: prometheus.NewDesc("asty_alloc_memory_mb",
			"RSS of the allocation's process, in MB.", common, nil),
		diskMB: prometheus.NewDesc("asty_alloc_disk_mb",
			"On-disk size of the service's subtree under work_dir, in MB.", common, nil),
		restartsTotal: prometheus.NewDesc("asty_alloc_restarts_total",
			"Number of times the agent has restarted the allocation since it was placed.", common, nil),
		uptimeSeconds: prometheus.NewDesc("asty_alloc_uptime_seconds",
			"Seconds since the allocation entered the running state. Zero when not yet started.", common, nil),
		health: prometheus.NewDesc("asty_alloc_health",
			"Latest health-probe result; value is always 1 — meaning lives on the `state` label (healthy/unhealthy/unknown).", healthLabels, nil),
		status: prometheus.NewDesc("asty_alloc_status",
			"Current allocation lifecycle status; value is always 1 — meaning lives on the `status` label.", statusLabels, nil),
	}
}

func (c *allocCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.cpuPercent
	ch <- c.memoryMB
	ch <- c.diskMB
	ch <- c.restartsTotal
	ch <- c.uptimeSeconds
	ch <- c.health
	ch <- c.status
}

func (c *allocCollector) Collect(ch chan<- prometheus.Metric) {
	snap := c.api.ctx.StreamHub().Snapshot()
	if snap == nil {
		return
	}
	now := time.Now()
	for _, allocs := range snap.AllocsByService {
		for _, a := range allocs {
			c.emit(ch, a, now)
		}
	}
}

func (c *allocCollector) emit(ch chan<- prometheus.Metric, a *types.ServiceAllocation, now time.Time) {
	g := func(d *prometheus.Desc, v float64) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v, a.ServiceName, a.NodeID, a.ID)
	}
	g(c.cpuPercent, float64(a.CPUUsage))
	g(c.memoryMB, float64(a.MemoryUsage))
	g(c.diskMB, float64(a.DiskUsage))
	g(c.restartsTotal, float64(a.Restarts))

	var uptime float64
	if !a.StartedAt.IsZero() && a.Status == types.AllocRunning {
		uptime = now.Sub(a.StartedAt).Seconds()
	}
	g(c.uptimeSeconds, uptime)

	healthState := string(a.HealthStatus)
	if healthState == "" {
		healthState = "unknown"
	}
	ch <- prometheus.MustNewConstMetric(c.health, prometheus.GaugeValue, 1,
		a.ServiceName, a.NodeID, a.ID, healthState)

	ch <- prometheus.MustNewConstMetric(c.status, prometheus.GaugeValue, 1,
		a.ServiceName, a.NodeID, a.ID, string(a.Status))
}
