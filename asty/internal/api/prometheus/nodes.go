package prometheus

import (
	"asty/asty/internal/core/types"

	prometheusclient "github.com/prometheus/client_golang/prometheus"
)

// nodeCollector is a prometheusclient.Collector that emits per-node gauges
// (`asty_node_*`) on every scrape. It pulls a fresh ClusterSnapshot
// from the stream hub each Collect call so the labels self-prune when
// nodes go away — no manual cleanup of stale label combinations.
type nodeCollector struct {
	ctx Context

	cpuTotal       *prometheusclient.Desc
	cpuAvailable   *prometheusclient.Desc
	memoryTotal    *prometheusclient.Desc
	memoryAvail    *prometheusclient.Desc
	diskTotal      *prometheusclient.Desc
	diskAvail      *prometheusclient.Desc
	diskType       *prometheusclient.Desc
	swapTotal      *prometheusclient.Desc
	swapAvail      *prometheusclient.Desc
	allocsRunning  *prometheusclient.Desc
	allocsPlanned  *prometheusclient.Desc
	status         *prometheusclient.Desc
	selfCPUPercent *prometheusclient.Desc
	selfMemoryMB   *prometheusclient.Desc
	selfDiskMB     *prometheusclient.Desc
}

func newNodeCollector(ctx Context) *nodeCollector {
	// `host` is the operator-provided public DNS name of the node
	// (NodeInfo.Host). Empty when the node is addressed by IP only —
	// still emitted as a label so the label set is stable across
	// scrapes (Prometheus rejects label additions between samples of
	// the same metric, not between scrapes).
	common := []string{"node_id", "datacenter", "host"}
	statusLabels := []string{"node_id", "datacenter", "host", "status"}
	diskTypeLabels := []string{"node_id", "datacenter", "host", "disk_type"}
	return &nodeCollector{
		ctx: ctx,
		cpuTotal: prometheusclient.NewDesc("asty_node_cpu_total_mhz",
			"Total CPU capacity reported by the node, in MHz.", common, nil),
		cpuAvailable: prometheusclient.NewDesc("asty_node_cpu_available_mhz",
			"CPU capacity not yet consumed by allocations, in MHz.", common, nil),
		memoryTotal: prometheusclient.NewDesc("asty_node_memory_total_mb",
			"Total RAM reported by the node, in MB.", common, nil),
		memoryAvail: prometheusclient.NewDesc("asty_node_memory_available_mb",
			"RAM not yet consumed by allocations, in MB.", common, nil),
		diskTotal: prometheusclient.NewDesc("asty_node_disk_total_mb",
			"Total capacity of the filesystem hosting the agent work_dir, in MB.", common, nil),
		diskAvail: prometheusclient.NewDesc("asty_node_disk_available_mb",
			"Available space on the filesystem hosting the agent work_dir, in MB.", common, nil),
		diskType: prometheusclient.NewDesc("asty_node_disk_type",
			"Physical disk class of the node; value is always 1 — the meaning lives on the `disk_type` label (ssd|hdd|unknown).", diskTypeLabels, nil),
		swapTotal: prometheusclient.NewDesc("asty_node_swap_total_mb",
			"Total swap configured on the node, in MB.", common, nil),
		swapAvail: prometheusclient.NewDesc("asty_node_swap_available_mb",
			"Swap currently free on the node, in MB.", common, nil),
		allocsRunning: prometheusclient.NewDesc("asty_node_allocations_running",
			"Allocations currently in the running state on the node.", common, nil),
		allocsPlanned: prometheusclient.NewDesc("asty_node_allocations_planned",
			"Allocations placed on the node (any live state, including pending/starting).", common, nil),
		status: prometheusclient.NewDesc("asty_node_status",
			"Current node lifecycle status; value is always 1 — the meaning lives on the `status` label.", statusLabels, nil),
		selfCPUPercent: prometheusclient.NewDesc("asty_node_self_cpu_percent",
			"CPU% consumed by the asty agent process itself on this node.", common, nil),
		selfMemoryMB: prometheusclient.NewDesc("asty_node_self_memory_mb",
			"RSS of the asty agent process itself on this node, in MB.", common, nil),
		selfDiskMB: prometheusclient.NewDesc("asty_node_self_disk_mb",
			"On-disk footprint of the agent work_dir (binaries + logs), in MB.", common, nil),
	}
}

func (c *nodeCollector) Describe(ch chan<- *prometheusclient.Desc) {
	ch <- c.cpuTotal
	ch <- c.cpuAvailable
	ch <- c.memoryTotal
	ch <- c.memoryAvail
	ch <- c.diskTotal
	ch <- c.diskAvail
	ch <- c.diskType
	ch <- c.swapTotal
	ch <- c.swapAvail
	ch <- c.allocsRunning
	ch <- c.allocsPlanned
	ch <- c.status
	ch <- c.selfCPUPercent
	ch <- c.selfMemoryMB
	ch <- c.selfDiskMB
}

func (c *nodeCollector) Collect(ch chan<- prometheusclient.Metric) {
	snap := c.ctx.StreamHub().Snapshot()
	if snap == nil {
		return
	}
	for _, n := range snap.Nodes {
		c.emit(ch, n)
	}
}

func (c *nodeCollector) emit(ch chan<- prometheusclient.Metric, n *types.NodeInfo) {
	g := func(d *prometheusclient.Desc, v float64) {
		ch <- prometheusclient.MustNewConstMetric(d, prometheusclient.GaugeValue, v, n.ID, n.Datacenter, n.Host)
	}
	g(c.cpuTotal, float64(n.CPUTotal))
	g(c.cpuAvailable, float64(n.CPUAvailable))
	g(c.memoryTotal, float64(n.MemoryTotal))
	g(c.memoryAvail, float64(n.MemoryAvailable))
	g(c.diskTotal, float64(n.DiskTotal))
	g(c.diskAvail, float64(n.DiskAvailable))
	g(c.swapTotal, float64(n.SwapTotal))
	g(c.swapAvail, float64(n.SwapAvailable))
	g(c.allocsRunning, float64(n.AllocationsRunning))
	g(c.allocsPlanned, float64(n.AllocationsPlanned))
	g(c.selfCPUPercent, n.SelfCPUPercent)
	g(c.selfMemoryMB, float64(n.SelfMemoryMB))
	g(c.selfDiskMB, float64(n.SelfDiskMB))

	ch <- prometheusclient.MustNewConstMetric(c.status, prometheusclient.GaugeValue, 1,
		n.ID, n.Datacenter, n.Host, string(n.Status))

	dt := n.DiskType
	if dt == "" {
		dt = types.DiskUnknown
	}
	ch <- prometheusclient.MustNewConstMetric(c.diskType, prometheusclient.GaugeValue, 1,
		n.ID, n.Datacenter, n.Host, string(dt))
}
