package rest

import (
	"asty/asty/internal/core/types"

	"github.com/prometheus/client_golang/prometheus"
)

// natsCollector emits per-node and cluster-aggregate NATS metrics
// scraped in-process by each agent from its local NATS monitoring port.
// Counters (in/out msgs) are exposed as CounterValue so `rate()` works
// out of the box in Prometheus.
type natsCollector struct {
	api *API

	cpuPercent    *prometheus.Desc
	memoryMB      *prometheus.Desc
	connections   *prometheus.Desc
	subscriptions *prometheus.Desc
	slowConsumers *prometheus.Desc
	inMsgs        *prometheus.Desc
	outMsgs       *prometheus.Desc
	jsMessages    *prometheus.Desc
	jsBytes       *prometheus.Desc
	diskMB        *prometheus.Desc

	clusterConnections *prometheus.Desc
	clusterJSMessages  *prometheus.Desc
	clusterJSBytes     *prometheus.Desc
}

func newNATSCollector(api *API) *natsCollector {
	common := []string{"node_id", "datacenter"}
	return &natsCollector{
		api: api,
		cpuPercent: prometheus.NewDesc("asty_node_nats_cpu_percent",
			"CPU% of the local NATS server (varz.cpu).", common, nil),
		memoryMB: prometheus.NewDesc("asty_node_nats_memory_mb",
			"RSS of the local NATS server, in MB (varz.mem).", common, nil),
		connections: prometheus.NewDesc("asty_node_nats_connections",
			"Active client connections to the local NATS (varz.connections).", common, nil),
		subscriptions: prometheus.NewDesc("asty_node_nats_subscriptions",
			"Current subscription count on the local NATS (varz.subscriptions).", common, nil),
		slowConsumers: prometheus.NewDesc("asty_node_nats_slow_consumers",
			"Slow-consumer events since NATS started; non-zero means clients are falling behind.", common, nil),
		inMsgs: prometheus.NewDesc("asty_node_nats_in_msgs_total",
			"Cumulative inbound messages on the local NATS server (varz.in_msgs).", common, nil),
		outMsgs: prometheus.NewDesc("asty_node_nats_out_msgs_total",
			"Cumulative outbound messages on the local NATS server (varz.out_msgs).", common, nil),
		jsMessages: prometheus.NewDesc("asty_node_nats_jetstream_messages",
			"Total messages held across JetStream streams (jsz.messages).", common, nil),
		jsBytes: prometheus.NewDesc("asty_node_nats_jetstream_bytes",
			"On-disk JetStream storage in bytes (jsz.bytes).", common, nil),
		diskMB: prometheus.NewDesc("asty_node_nats_disk_mb",
			"Total NATS disk footprint = binary baseline (synthetic in dev) + JetStream bytes.", common, nil),
		clusterConnections: prometheus.NewDesc("asty_cluster_nats_connections",
			"Sum of NATS client connections across all nodes.", nil, nil),
		clusterJSMessages: prometheus.NewDesc("asty_cluster_nats_jetstream_messages",
			"Sum of JetStream messages across all nodes.", nil, nil),
		clusterJSBytes: prometheus.NewDesc("asty_cluster_nats_jetstream_bytes",
			"Sum of JetStream on-disk size across all nodes, in bytes.", nil, nil),
	}
}

func (c *natsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.cpuPercent
	ch <- c.memoryMB
	ch <- c.connections
	ch <- c.subscriptions
	ch <- c.slowConsumers
	ch <- c.inMsgs
	ch <- c.outMsgs
	ch <- c.jsMessages
	ch <- c.jsBytes
	ch <- c.diskMB
	ch <- c.clusterConnections
	ch <- c.clusterJSMessages
	ch <- c.clusterJSBytes
}

func (c *natsCollector) Collect(ch chan<- prometheus.Metric) {
	snap := c.api.ctx.StreamHub().Snapshot()
	if snap == nil {
		return
	}
	var totalConns int
	var totalJSMsg, totalJSBytes int64
	for _, n := range snap.Nodes {
		c.emit(ch, n)
		totalConns += n.NATSConnections
		totalJSMsg += n.NATSJetStreamMessages
		totalJSBytes += n.NATSJetStreamBytes
	}
	ch <- prometheus.MustNewConstMetric(c.clusterConnections, prometheus.GaugeValue, float64(totalConns))
	ch <- prometheus.MustNewConstMetric(c.clusterJSMessages, prometheus.GaugeValue, float64(totalJSMsg))
	ch <- prometheus.MustNewConstMetric(c.clusterJSBytes, prometheus.GaugeValue, float64(totalJSBytes))
}

func (c *natsCollector) emit(ch chan<- prometheus.Metric, n *types.NodeInfo) {
	g := func(d *prometheus.Desc, v float64) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v, n.ID, n.Datacenter)
	}
	counter := func(d *prometheus.Desc, v float64) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.CounterValue, v, n.ID, n.Datacenter)
	}
	g(c.cpuPercent, n.NATSCPUPercent)
	g(c.memoryMB, float64(n.NATSMemoryMB))
	g(c.connections, float64(n.NATSConnections))
	g(c.subscriptions, float64(n.NATSSubscriptions))
	g(c.slowConsumers, float64(n.NATSSlowConsumers))
	counter(c.inMsgs, float64(n.NATSInMsgs))
	counter(c.outMsgs, float64(n.NATSOutMsgs))
	g(c.jsMessages, float64(n.NATSJetStreamMessages))
	g(c.jsBytes, float64(n.NATSJetStreamBytes))
	g(c.diskMB, float64(n.NATSDiskMB))
}
