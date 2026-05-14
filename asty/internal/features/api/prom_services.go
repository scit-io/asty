package api

import (
	"github.com/prometheus/client_golang/prometheus"
)

// serviceCollector emits per-service gauges (`asty_service_*`).
// All fields come straight from ServiceWithUsage on the cluster
// snapshot, which is what the UI's services pages render.
type serviceCollector struct {
	api *API

	copiesCurrent      *prometheus.Desc
	minCopies          *prometheus.Desc
	cpuAvgPercent      *prometheus.Desc
	memoryAvgMB        *prometheus.Desc
	cooldownUpActive   *prometheus.Desc
	cooldownDownActive *prometheus.Desc
}

func newServiceCollector(api *API) *serviceCollector {
	labels := []string{"service"}
	return &serviceCollector{
		api: api,
		copiesCurrent: prometheus.NewDesc("asty_service_copies_current",
			"Number of live allocations (pending/starting/running) for the service.", labels, nil),
		minCopies: prometheus.NewDesc("asty_service_min_copies",
			"Floor for scale-down, taken from the service definition's autoscale.min_copies.", labels, nil),
		cpuAvgPercent: prometheus.NewDesc("asty_service_cpu_avg_percent",
			"Mean CPU% across the service's running allocations.", labels, nil),
		memoryAvgMB: prometheus.NewDesc("asty_service_memory_avg_mb",
			"Mean RSS (MB) across the service's running allocations.", labels, nil),
		cooldownUpActive: prometheus.NewDesc("asty_service_cooldown_up_active",
			"1 while scale-up is suppressed by the cooldown window; 0 otherwise.", labels, nil),
		cooldownDownActive: prometheus.NewDesc("asty_service_cooldown_down_active",
			"1 while scale-down is suppressed by the cooldown window; 0 otherwise.", labels, nil),
	}
}

func (c *serviceCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.copiesCurrent
	ch <- c.minCopies
	ch <- c.cpuAvgPercent
	ch <- c.memoryAvgMB
	ch <- c.cooldownUpActive
	ch <- c.cooldownDownActive
}

func (c *serviceCollector) Collect(ch chan<- prometheus.Metric) {
	snap := c.api.ctx.StreamHub().Snapshot()
	if snap == nil {
		return
	}
	for _, s := range snap.Services {
		if s.ServiceDefinition == nil {
			continue
		}
		name := s.ServiceDefinition.Name
		g := func(d *prometheus.Desc, v float64) {
			ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v, name)
		}
		g(c.copiesCurrent, float64(s.CurrentCopies))
		g(c.minCopies, float64(s.MinCopies))
		g(c.cpuAvgPercent, s.AvgCPUPercent)
		g(c.memoryAvgMB, s.AvgMemoryMB)
		g(c.cooldownUpActive, boolToFloat(s.CooldownUpActive))
		g(c.cooldownDownActive, boolToFloat(s.CooldownDownActive))
	}
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
