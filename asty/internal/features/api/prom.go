package api

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// initProm wires a private prometheus.Registry to the API instance and
// registers the orchestrator-side instruments. A per-instance registry
// (instead of prometheus.DefaultRegisterer) keeps the gateway's metrics
// — exposed on its own :8081/metrics — separate, and avoids the
// double-register panic during tests that spin up multiple APIs.
//
// Mirror rule: every gauge here corresponds to something the web UI
// also displays. When the UI gains a metric, add the matching gauge
// here in the same PR (see CLAUDE.md > Observability).
func (api *API) initProm() {
	reg := prometheus.NewRegistry()

	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	reg.MustRegister(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "asty_cluster_nodes_total",
			Help: "Total number of nodes known to the orchestrator (cluster.nodes_total in the UI).",
		},
		func() float64 {
			nodes, err := api.ctx.ClusterState().ListNodes()
			if err != nil {
				return 0
			}
			return float64(len(nodes))
		},
	))

	reg.MustRegister(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "asty_cluster_nodes_healthy",
			Help: "Number of nodes whose last_seen is within the staleness window (cluster.nodes_healthy in the UI).",
		},
		func() float64 {
			nodes, err := api.ctx.ClusterState().ListNodes()
			if err != nil {
				return 0
			}
			now := time.Now()
			healthy := 0
			for _, n := range nodes {
				if n.IsHealthy(now) {
					healthy++
				}
			}
			return float64(healthy)
		},
	))

	reg.MustRegister(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "asty_cluster_services_loaded",
			Help: "Number of service definitions currently loaded (services.loaded in the UI).",
		},
		func() float64 {
			return float64(len(api.ctx.Services()))
		},
	))

	// Per-resource collectors pull a fresh snapshot on each scrape, so
	// labels for departed nodes/allocations disappear automatically.
	reg.MustRegister(newNodeCollector(api))
	reg.MustRegister(newAllocCollector(api))

	api.promHandler = promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
}
