package prometheus

import (
	"net/http"
	"time"

	prometheusclient "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handler builds the orchestrator's /metrics http.Handler with a fresh
// private prometheusclient.Registry. Every Asty-specific collector lives in
// this package; callers (api/rest's router) mount the returned handler
// at the exposition path. A per-Handler registry (rather than
// prometheusclient.DefaultRegisterer) avoids the double-register panic
// in tests that spin up multiple instances.
//
// Mirror rule: every gauge here corresponds to something the web UI
// also displays. When the UI gains a metric, add the matching gauge
// here in the same PR.
func Handler(ctx Context) http.Handler {
	reg := prometheusclient.NewRegistry()

	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	reg.MustRegister(prometheusclient.NewGaugeFunc(
		prometheusclient.GaugeOpts{
			Name: "asty_cluster_nodes_total",
			Help: "Total number of nodes known to the orchestrator (cluster.nodes_total in the UI).",
		},
		func() float64 {
			nodes, err := ctx.ClusterState().ListNodes()
			if err != nil {
				return 0
			}
			return float64(len(nodes))
		},
	))

	reg.MustRegister(prometheusclient.NewGaugeFunc(
		prometheusclient.GaugeOpts{
			Name: "asty_cluster_nodes_healthy",
			Help: "Number of nodes whose last_seen is within the staleness window (cluster.nodes_healthy in the UI).",
		},
		func() float64 {
			nodes, err := ctx.ClusterState().ListNodes()
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

	reg.MustRegister(prometheusclient.NewGaugeFunc(
		prometheusclient.GaugeOpts{
			Name: "asty_cluster_services_loaded",
			Help: "Number of service definitions currently loaded (services.loaded in the UI).",
		},
		func() float64 {
			return float64(len(ctx.Services()))
		},
	))

	reg.MustRegister(newClusterCollector(ctx))
	reg.MustRegister(newNodeCollector(ctx))
	reg.MustRegister(newAllocCollector(ctx))
	reg.MustRegister(newServiceCollector(ctx))
	reg.MustRegister(newDeployCollector(ctx))
	reg.MustRegister(newNATSCollector(ctx))

	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
}
