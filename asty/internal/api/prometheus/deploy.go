package prometheus

import (
	"asty/asty/internal/ops/deployer"

	prometheusclient "github.com/prometheus/client_golang/prometheus"
)

// deployCollector emits per-service deployment gauges. It walks the
// deployer's history newest-first, keeping the most recent record per
// service so the UI's deploy panel and Prometheus see the same row.
type deployCollector struct {
	ctx Context

	state    *prometheusclient.Desc
	progress *prometheusclient.Desc
}

func newDeployCollector(ctx Context) *deployCollector {
	return &deployCollector{
		ctx: ctx,
		state: prometheusclient.NewDesc("asty_deploy_state",
			"Latest deployment state for the service; value is always 1 — meaning lives on the `state` label.",
			[]string{"service", "state"}, nil),
		progress: prometheusclient.NewDesc("asty_deploy_progress_percent",
			"Progress (0..100) of the latest deployment for the service.",
			[]string{"service"}, nil),
	}
}

func (c *deployCollector) Describe(ch chan<- *prometheusclient.Desc) {
	ch <- c.state
	ch <- c.progress
}

func (c *deployCollector) Collect(ch chan<- prometheusclient.Metric) {
	dep := c.ctx.Deployer()
	if dep == nil {
		return
	}
	latest := make(map[string]deployer.DeploymentRecord)
	for _, r := range dep.GetHistory() {
		if _, seen := latest[r.Service]; !seen {
			latest[r.Service] = r
		}
	}
	for _, r := range latest {
		ch <- prometheusclient.MustNewConstMetric(c.state, prometheusclient.GaugeValue, 1,
			r.Service, string(r.Status))
		ch <- prometheusclient.MustNewConstMetric(c.progress, prometheusclient.GaugeValue,
			float64(r.Progress), r.Service)
	}
}
