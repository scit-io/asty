package prometheus

import (
	prometheusclient "github.com/prometheus/client_golang/prometheus"
)

// serviceCollector emits per-service gauges (`asty_service_*`).
// All fields come straight from ServiceWithUsage on the cluster
// snapshot, which is what the UI's services pages render.
type serviceCollector struct {
	ctx Context

	copiesCurrent      *prometheusclient.Desc
	minCopies          *prometheusclient.Desc
	cpuAvgPercent      *prometheusclient.Desc
	memoryAvgMB        *prometheusclient.Desc
	cooldownUpActive   *prometheusclient.Desc
	cooldownDownActive *prometheusclient.Desc
}

func newServiceCollector(ctx Context) *serviceCollector {
	labels := []string{"service"}
	return &serviceCollector{
		ctx:           ctx,
		copiesCurrent: prometheusclient.NewDesc("asty_service_copies_current",
			"Number of live allocations (pending/starting/running) for the service.", labels, nil),
		minCopies: prometheusclient.NewDesc("asty_service_min_copies",
			"Floor for scale-down, taken from the service definition's autoscale.min_copies.", labels, nil),
		cpuAvgPercent: prometheusclient.NewDesc("asty_service_cpu_avg_percent",
			"Mean CPU% across the service's running allocations.", labels, nil),
		memoryAvgMB: prometheusclient.NewDesc("asty_service_memory_avg_mb",
			"Mean RSS (MB) across the service's running allocations.", labels, nil),
		cooldownUpActive: prometheusclient.NewDesc("asty_service_cooldown_up_active",
			"1 while scale-up is suppressed by the cooldown window; 0 otherwise.", labels, nil),
		cooldownDownActive: prometheusclient.NewDesc("asty_service_cooldown_down_active",
			"1 while scale-down is suppressed by the cooldown window; 0 otherwise.", labels, nil),
	}
}

func (c *serviceCollector) Describe(ch chan<- *prometheusclient.Desc) {
	ch <- c.copiesCurrent
	ch <- c.minCopies
	ch <- c.cpuAvgPercent
	ch <- c.memoryAvgMB
	ch <- c.cooldownUpActive
	ch <- c.cooldownDownActive
}

func (c *serviceCollector) Collect(ch chan<- prometheusclient.Metric) {
	snap := c.ctx.StreamHub().Snapshot()
	if snap == nil {
		return
	}
	for _, s := range snap.Services {
		if s.ServiceDefinition == nil {
			continue
		}
		name := s.ServiceDefinition.Name
		g := func(d *prometheusclient.Desc, v float64) {
			ch <- prometheusclient.MustNewConstMetric(d, prometheusclient.GaugeValue, v, name)
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
