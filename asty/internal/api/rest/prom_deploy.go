package rest

import (
	"asty/asty/internal/ops/deployer"

	"github.com/prometheus/client_golang/prometheus"
)

// deployCollector emits per-service deployment gauges. It walks the
// deployer's history newest-first, keeping the most recent record per
// service so the UI's deploy panel and Prometheus see the same row.
type deployCollector struct {
	api *API

	state    *prometheus.Desc
	progress *prometheus.Desc
}

func newDeployCollector(api *API) *deployCollector {
	return &deployCollector{
		api: api,
		state: prometheus.NewDesc("asty_deploy_state",
			"Latest deployment state for the service; value is always 1 — meaning lives on the `state` label.",
			[]string{"service", "state"}, nil),
		progress: prometheus.NewDesc("asty_deploy_progress_percent",
			"Progress (0..100) of the latest deployment for the service.",
			[]string{"service"}, nil),
	}
}

func (c *deployCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.state
	ch <- c.progress
}

func (c *deployCollector) Collect(ch chan<- prometheus.Metric) {
	dep := c.api.ctx.Deployer()
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
		ch <- prometheus.MustNewConstMetric(c.state, prometheus.GaugeValue, 1,
			r.Service, string(r.Status))
		ch <- prometheus.MustNewConstMetric(c.progress, prometheus.GaugeValue,
			float64(r.Progress), r.Service)
	}
}
