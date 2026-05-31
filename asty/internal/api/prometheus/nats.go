package prometheus

import (
	"asty/asty/internal/core/types"

	prometheusclient "github.com/prometheus/client_golang/prometheus"
)

// natsCollector emits per-node and cluster-aggregate NATS metrics
// scraped in-process by each agent from its local NATS monitoring port.
// Counters (in/out msgs) are exposed as CounterValue so `rate()` works
// out of the box in Prometheus.
type natsCollector struct {
	ctx Context

	cpuPercent    *prometheusclient.Desc
	memoryMB      *prometheusclient.Desc
	connections   *prometheusclient.Desc
	subscriptions *prometheusclient.Desc
	slowConsumers *prometheusclient.Desc
	inMsgs        *prometheusclient.Desc
	outMsgs       *prometheusclient.Desc
	jsMessages    *prometheusclient.Desc
	jsBytes       *prometheusclient.Desc
	diskMB        *prometheusclient.Desc

	clusterConnections *prometheusclient.Desc
	clusterJSMessages  *prometheusclient.Desc
	clusterJSBytes     *prometheusclient.Desc
}

func newNATSCollector(ctx Context) *natsCollector {
	// host = NodeInfo.Host — operator-provided public DNS name, kept
	// in sync with nodeCollector's label set for consistent join-keys
	// when correlating asty_node_* and asty_node_nats_*.
	common := []string{"node_id", "datacenter", "host"}
	return &natsCollector{
		ctx: ctx,
		cpuPercent: prometheusclient.NewDesc("asty_node_nats_cpu_percent",
			"CPU% of the local NATS server (varz.cpu).", common, nil),
		memoryMB: prometheusclient.NewDesc("asty_node_nats_memory_mb",
			"RSS of the local NATS server, in MB (varz.mem).", common, nil),
		connections: prometheusclient.NewDesc("asty_node_nats_connections",
			"Active client connections to the local NATS (varz.connections).", common, nil),
		subscriptions: prometheusclient.NewDesc("asty_node_nats_subscriptions",
			"Current subscription count on the local NATS (varz.subscriptions).", common, nil),
		slowConsumers: prometheusclient.NewDesc("asty_node_nats_slow_consumers",
			"Slow-consumer events since NATS started; non-zero means clients are falling behind.", common, nil),
		inMsgs: prometheusclient.NewDesc("asty_node_nats_in_msgs_total",
			"Cumulative inbound messages on the local NATS server (varz.in_msgs).", common, nil),
		outMsgs: prometheusclient.NewDesc("asty_node_nats_out_msgs_total",
			"Cumulative outbound messages on the local NATS server (varz.out_msgs).", common, nil),
		jsMessages: prometheusclient.NewDesc("asty_node_nats_jetstream_messages",
			"Total messages held across JetStream streams (jsz.messages).", common, nil),
		jsBytes: prometheusclient.NewDesc("asty_node_nats_jetstream_bytes",
			"On-disk JetStream storage in bytes (jsz.bytes).", common, nil),
		diskMB: prometheusclient.NewDesc("asty_node_nats_disk_mb",
			"Total NATS disk footprint = binary baseline (synthetic in dev) + JetStream bytes.", common, nil),
		clusterConnections: prometheusclient.NewDesc("asty_cluster_nats_connections",
			"Sum of NATS client connections across all nodes.", nil, nil),
		clusterJSMessages: prometheusclient.NewDesc("asty_cluster_nats_jetstream_messages",
			"Sum of JetStream messages across all nodes.", nil, nil),
		clusterJSBytes: prometheusclient.NewDesc("asty_cluster_nats_jetstream_bytes",
			"Sum of JetStream on-disk size across all nodes, in bytes.", nil, nil),
	}
}

func (c *natsCollector) Describe(ch chan<- *prometheusclient.Desc) {
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

func (c *natsCollector) Collect(ch chan<- prometheusclient.Metric) {
	snap := c.ctx.StreamHub().Snapshot()
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
	ch <- prometheusclient.MustNewConstMetric(c.clusterConnections, prometheusclient.GaugeValue, float64(totalConns))
	ch <- prometheusclient.MustNewConstMetric(c.clusterJSMessages, prometheusclient.GaugeValue, float64(totalJSMsg))
	ch <- prometheusclient.MustNewConstMetric(c.clusterJSBytes, prometheusclient.GaugeValue, float64(totalJSBytes))
}

func (c *natsCollector) emit(ch chan<- prometheusclient.Metric, n *types.NodeInfo) {
	g := func(d *prometheusclient.Desc, v float64) {
		ch <- prometheusclient.MustNewConstMetric(d, prometheusclient.GaugeValue, v, n.ID, n.Datacenter, n.Host)
	}
	counter := func(d *prometheusclient.Desc, v float64) {
		ch <- prometheusclient.MustNewConstMetric(d, prometheusclient.CounterValue, v, n.ID, n.Datacenter, n.Host)
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
