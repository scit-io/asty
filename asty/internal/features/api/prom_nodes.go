package api

import (
	"asty/asty/internal/core/types"

	"github.com/prometheus/client_golang/prometheus"
)

// nodeCollector is a prometheus.Collector that emits per-node gauges
// (`asty_node_*`) on every scrape. It pulls a fresh ClusterSnapshot
// from the stream hub each Collect call so the labels self-prune when
// nodes go away — no manual cleanup of stale label combinations.
type nodeCollector struct {
	api *API

	cpuTotal       *prometheus.Desc
	cpuAvailable   *prometheus.Desc
	memoryTotal    *prometheus.Desc
	memoryAvail    *prometheus.Desc
	diskTotal      *prometheus.Desc
	diskAvail      *prometheus.Desc
	allocsRunning  *prometheus.Desc
	allocsPlanned  *prometheus.Desc
	status         *prometheus.Desc
	selfCPUPercent *prometheus.Desc
	selfMemoryMB   *prometheus.Desc
	selfDiskMB     *prometheus.Desc
}

func newNodeCollector(api *API) *nodeCollector {
	common := []string{"node_id", "datacenter"}
	statusLabels := []string{"node_id", "datacenter", "status"}
	return &nodeCollector{
		api: api,
		cpuTotal: prometheus.NewDesc("asty_node_cpu_total_mhz",
			"Total CPU capacity reported by the node, in MHz.", common, nil),
		cpuAvailable: prometheus.NewDesc("asty_node_cpu_available_mhz",
			"CPU capacity not yet consumed by allocations, in MHz.", common, nil),
		memoryTotal: prometheus.NewDesc("asty_node_memory_total_mb",
			"Total RAM reported by the node, in MB.", common, nil),
		memoryAvail: prometheus.NewDesc("asty_node_memory_available_mb",
			"RAM not yet consumed by allocations, in MB.", common, nil),
		diskTotal: prometheus.NewDesc("asty_node_disk_total_mb",
			"Total capacity of the filesystem hosting the agent work_dir, in MB.", common, nil),
		diskAvail: prometheus.NewDesc("asty_node_disk_available_mb",
			"Available space on the filesystem hosting the agent work_dir, in MB.", common, nil),
		allocsRunning: prometheus.NewDesc("asty_node_allocations_running",
			"Allocations currently in the running state on the node.", common, nil),
		allocsPlanned: prometheus.NewDesc("asty_node_allocations_planned",
			"Allocations placed on the node (any live state, including pending/starting).", common, nil),
		status: prometheus.NewDesc("asty_node_status",
			"Current node lifecycle status; value is always 1 — the meaning lives on the `status` label.", statusLabels, nil),
		selfCPUPercent: prometheus.NewDesc("asty_node_self_cpu_percent",
			"CPU% consumed by the asty agent process itself on this node.", common, nil),
		selfMemoryMB: prometheus.NewDesc("asty_node_self_memory_mb",
			"RSS of the asty agent process itself on this node, in MB.", common, nil),
		selfDiskMB: prometheus.NewDesc("asty_node_self_disk_mb",
			"On-disk footprint of the agent work_dir (binaries + logs), in MB.", common, nil),
	}
}

func (c *nodeCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.cpuTotal
	ch <- c.cpuAvailable
	ch <- c.memoryTotal
	ch <- c.memoryAvail
	ch <- c.diskTotal
	ch <- c.diskAvail
	ch <- c.allocsRunning
	ch <- c.allocsPlanned
	ch <- c.status
	ch <- c.selfCPUPercent
	ch <- c.selfMemoryMB
	ch <- c.selfDiskMB
}

func (c *nodeCollector) Collect(ch chan<- prometheus.Metric) {
	snap := c.api.ctx.StreamHub().Snapshot()
	if snap == nil {
		return
	}
	for _, n := range snap.Nodes {
		c.emit(ch, n)
	}
}

func (c *nodeCollector) emit(ch chan<- prometheus.Metric, n *types.NodeInfo) {
	g := func(d *prometheus.Desc, v float64) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v, n.ID, n.Datacenter)
	}
	g(c.cpuTotal, float64(n.CPUTotal))
	g(c.cpuAvailable, float64(n.CPUAvailable))
	g(c.memoryTotal, float64(n.MemoryTotal))
	g(c.memoryAvail, float64(n.MemoryAvailable))
	g(c.diskTotal, float64(n.DiskTotal))
	g(c.diskAvail, float64(n.DiskAvailable))
	g(c.allocsRunning, float64(n.AllocationsRunning))
	g(c.allocsPlanned, float64(n.AllocationsPlanned))
	g(c.selfCPUPercent, n.SelfCPUPercent)
	g(c.selfMemoryMB, float64(n.SelfMemoryMB))
	g(c.selfDiskMB, float64(n.SelfDiskMB))

	ch <- prometheus.MustNewConstMetric(c.status, prometheus.GaugeValue, 1,
		n.ID, n.Datacenter, string(n.Status))
}
