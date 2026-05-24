package prometheus

import (
	"time"

	"asty/asty/internal/core/types"

	prometheusclient "github.com/prometheus/client_golang/prometheus"
)

// allocCollector emits per-allocation gauges (`asty_alloc_*`). Like
// nodeCollector it pulls a fresh snapshot per scrape so allocations
// that disappear stop being reported automatically.
type allocCollector struct {
	ctx Context

	cpuPercent    *prometheusclient.Desc
	memoryMB      *prometheusclient.Desc
	diskMB        *prometheusclient.Desc
	rps           *prometheusclient.Desc
	restartsTotal *prometheusclient.Desc
	uptimeSeconds *prometheusclient.Desc
	health        *prometheusclient.Desc
	status        *prometheusclient.Desc
}

func newAllocCollector(ctx Context) *allocCollector {
	common := []string{"service", "node_id", "alloc_id"}
	healthLabels := []string{"service", "node_id", "alloc_id", "state"}
	statusLabels := []string{"service", "node_id", "alloc_id", "status"}
	return &allocCollector{
		ctx:           ctx,
		cpuPercent: prometheusclient.NewDesc("asty_alloc_cpu_percent",
			"CPU% consumed by the allocation's process.", common, nil),
		memoryMB: prometheusclient.NewDesc("asty_alloc_memory_mb",
			"RSS of the allocation's process, in MB.", common, nil),
		diskMB: prometheusclient.NewDesc("asty_alloc_disk_mb",
			"On-disk size of the service's subtree under work_dir, in MB.", common, nil),
		rps: prometheusclient.NewDesc("asty_alloc_rps",
			"Latest per-(node, service) RPS attributable to the allocation. Sourced from the local gateway's per-service surviving-request delta; same number as the allocation page's RPS chart.", common, nil),
		restartsTotal: prometheusclient.NewDesc("asty_alloc_restarts_total",
			"Number of times the agent has restarted the allocation since it was placed.", common, nil),
		uptimeSeconds: prometheusclient.NewDesc("asty_alloc_uptime_seconds",
			"Seconds since the allocation entered the running state. Zero when not yet started.", common, nil),
		health: prometheusclient.NewDesc("asty_alloc_health",
			"Latest health-probe result; value is always 1 — meaning lives on the `state` label (healthy/unhealthy/unknown).", healthLabels, nil),
		status: prometheusclient.NewDesc("asty_alloc_status",
			"Current allocation lifecycle status; value is always 1 — meaning lives on the `status` label.", statusLabels, nil),
	}
}

func (c *allocCollector) Describe(ch chan<- *prometheusclient.Desc) {
	ch <- c.cpuPercent
	ch <- c.memoryMB
	ch <- c.diskMB
	ch <- c.rps
	ch <- c.restartsTotal
	ch <- c.uptimeSeconds
	ch <- c.health
	ch <- c.status
}

func (c *allocCollector) Collect(ch chan<- prometheusclient.Metric) {
	snap := c.ctx.StreamHub().Snapshot()
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

func (c *allocCollector) emit(ch chan<- prometheusclient.Metric, a *types.ServiceAllocation, now time.Time) {
	g := func(d *prometheusclient.Desc, v float64) {
		ch <- prometheusclient.MustNewConstMetric(d, prometheusclient.GaugeValue, v, a.ServiceName, a.NodeID, a.ID)
	}
	g(c.cpuPercent, float64(a.CPUUsage))
	g(c.memoryMB, float64(a.MemoryUsage))
	g(c.diskMB, float64(a.DiskUsage))
	var rps float64
	if store := c.ctx.MetricsStore(); store != nil {
		rps = store.GetLatestServiceRPS(a.NodeID, a.ServiceName)
	}
	g(c.rps, rps)
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
	ch <- prometheusclient.MustNewConstMetric(c.health, prometheusclient.GaugeValue, 1,
		a.ServiceName, a.NodeID, a.ID, healthState)

	ch <- prometheusclient.MustNewConstMetric(c.status, prometheusclient.GaugeValue, 1,
		a.ServiceName, a.NodeID, a.ID, string(a.Status))
}
