package api

import (
	"asty/asty/internal/core/types"

	"github.com/prometheus/client_golang/prometheus"
)

// clusterCollector emits cluster-wide aggregates and the leader gauge.
// Per-scrape snapshot semantics keep it consistent with nodeCollector
// and allocCollector — values reflect the same state the UI sees.
type clusterCollector struct {
	api *API

	cpuTotal       *prometheus.Desc
	cpuAvailable   *prometheus.Desc
	cpuUsed        *prometheus.Desc
	memoryTotal    *prometheus.Desc
	memoryAvail    *prometheus.Desc
	memoryUsed     *prometheus.Desc
	diskTotal      *prometheus.Desc
	diskAvail      *prometheus.Desc
	diskUsed       *prometheus.Desc
	disksSSD       *prometheus.Desc
	disksHDD       *prometheus.Desc
	disksUnknown   *prometheus.Desc
	swapTotal      *prometheus.Desc
	swapAvail      *prometheus.Desc
	swapUsed       *prometheus.Desc
	rps            *prometheus.Desc
	healthPercent  *prometheus.Desc
	leader         *prometheus.Desc
}

func newClusterCollector(api *API) *clusterCollector {
	return &clusterCollector{
		api:           api,
		cpuTotal:      prometheus.NewDesc("asty_cluster_cpu_total_mhz", "Sum of CPUTotal across all nodes, MHz.", nil, nil),
		cpuAvailable:  prometheus.NewDesc("asty_cluster_cpu_available_mhz", "Sum of CPUAvailable across all nodes, MHz.", nil, nil),
		cpuUsed:       prometheus.NewDesc("asty_cluster_cpu_used_mhz", "CPUTotal − CPUAvailable across all nodes, MHz.", nil, nil),
		memoryTotal:   prometheus.NewDesc("asty_cluster_memory_total_mb", "Sum of MemoryTotal across all nodes, MB.", nil, nil),
		memoryAvail:   prometheus.NewDesc("asty_cluster_memory_available_mb", "Sum of MemoryAvailable across all nodes, MB.", nil, nil),
		memoryUsed:    prometheus.NewDesc("asty_cluster_memory_used_mb", "MemoryTotal − MemoryAvailable across all nodes, MB.", nil, nil),
		diskTotal:     prometheus.NewDesc("asty_cluster_disk_total_mb", "Sum of DiskTotal across all nodes, MB.", nil, nil),
		diskAvail:     prometheus.NewDesc("asty_cluster_disk_available_mb", "Sum of DiskAvailable across all nodes, MB.", nil, nil),
		diskUsed:      prometheus.NewDesc("asty_cluster_disk_used_mb", "DiskTotal − DiskAvailable across all nodes, MB.", nil, nil),
		disksSSD:      prometheus.NewDesc("asty_cluster_disks_ssd", "Number of nodes whose work_dir disk is solid-state.", nil, nil),
		disksHDD:      prometheus.NewDesc("asty_cluster_disks_hdd", "Number of nodes whose work_dir disk is rotational.", nil, nil),
		disksUnknown:  prometheus.NewDesc("asty_cluster_disks_unknown", "Number of nodes whose disk class couldn't be detected.", nil, nil),
		swapTotal:     prometheus.NewDesc("asty_cluster_swap_total_mb", "Sum of SwapTotal across all nodes, MB.", nil, nil),
		swapAvail:     prometheus.NewDesc("asty_cluster_swap_available_mb", "Sum of SwapAvailable across all nodes, MB.", nil, nil),
		swapUsed:      prometheus.NewDesc("asty_cluster_swap_used_mb", "SwapTotal − SwapAvailable across all nodes, MB.", nil, nil),
		rps:           prometheus.NewDesc("asty_cluster_rps", "Sum of latest valid-RPS samples across all nodes.", nil, nil),
		healthPercent: prometheus.NewDesc("asty_cluster_health_percent", "Percentage of nodes whose last_seen is within the staleness window.", nil, nil),
		leader:        prometheus.NewDesc("asty_leader", "1 on the node that currently holds the leader lease; the label carries its ID.", []string{"node_id"}, nil),
	}
}

func (c *clusterCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.cpuTotal
	ch <- c.cpuAvailable
	ch <- c.cpuUsed
	ch <- c.memoryTotal
	ch <- c.memoryAvail
	ch <- c.memoryUsed
	ch <- c.diskTotal
	ch <- c.diskAvail
	ch <- c.diskUsed
	ch <- c.disksSSD
	ch <- c.disksHDD
	ch <- c.disksUnknown
	ch <- c.swapTotal
	ch <- c.swapAvail
	ch <- c.swapUsed
	ch <- c.rps
	ch <- c.healthPercent
	ch <- c.leader
}

func (c *clusterCollector) Collect(ch chan<- prometheus.Metric) {
	snap := c.api.ctx.StreamHub().Snapshot()
	if snap == nil {
		return
	}

	var cpuT, cpuA int64
	var memT, memA, diskT, diskA, swapT, swapA int64
	var ssd, hdd, unk int
	for _, n := range snap.Nodes {
		cpuT += int64(n.CPUTotal)
		cpuA += int64(n.CPUAvailable)
		memT += n.MemoryTotal
		memA += n.MemoryAvailable
		diskT += n.DiskTotal
		diskA += n.DiskAvailable
		swapT += n.SwapTotal
		swapA += n.SwapAvailable
		switch n.DiskType {
		case types.DiskSSD:
			ssd++
		case types.DiskHDD:
			hdd++
		default:
			unk++
		}
	}
	g := func(d *prometheus.Desc, v float64) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v)
	}
	g(c.cpuTotal, float64(cpuT))
	g(c.cpuAvailable, float64(cpuA))
	g(c.cpuUsed, float64(cpuT-cpuA))
	g(c.memoryTotal, float64(memT))
	g(c.memoryAvail, float64(memA))
	g(c.memoryUsed, float64(memT-memA))
	g(c.diskTotal, float64(diskT))
	g(c.diskAvail, float64(diskA))
	g(c.diskUsed, float64(diskT-diskA))
	g(c.disksSSD, float64(ssd))
	g(c.disksHDD, float64(hdd))
	g(c.disksUnknown, float64(unk))
	g(c.swapTotal, float64(swapT))
	g(c.swapAvail, float64(swapA))
	g(c.swapUsed, float64(swapT-swapA))

	var rps float64
	if store := c.api.ctx.MetricsStore(); store != nil {
		for _, n := range snap.Nodes {
			rps += store.GetLatestRPS(n.ID)
		}
	}
	g(c.rps, rps)

	healthPct := 0.0
	if snap.Cluster.NodesTotal > 0 {
		healthPct = float64(snap.Cluster.NodesHealthy) / float64(snap.Cluster.NodesTotal) * 100
	}
	g(c.healthPercent, healthPct)

	if leaderID := snap.Cluster.Leader; leaderID != "" {
		ch <- prometheus.MustNewConstMetric(c.leader, prometheus.GaugeValue, 1, leaderID)
	}
}
