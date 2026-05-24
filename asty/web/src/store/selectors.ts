import { useMemo } from 'react'
import type { MetricPoint, Node } from '@/types'
import { useClusterStore } from './index'

// ClusterAggregates rolls per-node samples into the three sub-blocks
// the Cluster Overview renders: cluster-wide resources + asty
// (orchestrator process) + nats. Same numbers shape the cluster page's
// tiles and the shared <NatsTiles> block.
export interface ClusterAggregates {
  cluster: {
    cpuUsage: number
    cpuTotal: number
    memoryUsage: number
    memoryTotal: number
    diskUsage: number
    diskTotal: number
    rps: number
  }
  asty: {
    cpuUsage: number
    cpuTotal: number
    memoryUsage: number
    memoryTotal: number
    diskUsage: number
    diskTotal: number
  }
  nats: {
    cpuUsage: number
    cpuTotal: number
    memoryUsage: number
    memoryTotal: number
    diskUsage: number
    diskTotal: number
    connections: number
    subscriptions: number
    slow: number
    inMsgs: number
    outMsgs: number
    jsMessages: number
  }
}

function computeClusterAggregates(nodes: Node[], rpsMetrics: MetricPoint[]): ClusterAggregates {
  let cpuT = 0, cpuA = 0, memT = 0, memA = 0, diskT = 0, diskA = 0
  let selfCPU = 0, selfMem = 0, selfDisk = 0
  let natsCPU = 0, natsMem = 0, natsConn = 0, natsDisk = 0
  let natsSubs = 0, natsSlow = 0, natsIn = 0, natsOut = 0, natsJSMsgs = 0
  for (const n of nodes) {
    cpuT += n.cpu_total
    cpuA += n.cpu_available
    memT += n.memory_total
    memA += n.memory_available
    diskT += n.disk_total
    diskA += n.disk_available
    selfCPU += n.self_cpu_percent
    selfMem += n.self_memory_mb
    selfDisk += n.self_disk_mb
    natsCPU += n.nats_cpu_percent
    natsMem += n.nats_memory_mb
    natsConn += n.nats_connections
    natsDisk += n.nats_disk_mb
    natsSubs += n.nats_subscriptions
    natsSlow += n.nats_slow_consumers
    natsIn += n.nats_in_msgs
    natsOut += n.nats_out_msgs
    natsJSMsgs += n.nats_jetstream_messages
  }
  const lastRps = rpsMetrics.length ? rpsMetrics[rpsMetrics.length - 1].value : 0
  return {
    cluster: {
      cpuUsage: cpuT - cpuA, cpuTotal: cpuT,
      memoryUsage: memT - memA, memoryTotal: memT,
      diskUsage: diskT - diskA, diskTotal: diskT,
      rps: lastRps,
    },
    asty: {
      cpuUsage: selfCPU, cpuTotal: 100 * nodes.length,
      memoryUsage: selfMem, memoryTotal: memT,
      diskUsage: selfDisk, diskTotal: diskT,
    },
    nats: {
      cpuUsage: natsCPU, cpuTotal: 100 * nodes.length,
      memoryUsage: natsMem, memoryTotal: memT,
      diskUsage: natsDisk, diskTotal: diskT,
      connections: natsConn, subscriptions: natsSubs, slow: natsSlow,
      inMsgs: natsIn, outMsgs: natsOut, jsMessages: natsJSMsgs,
    },
  }
}

// useClusterAggregates folds the per-node samples into the cluster-
// overview shape. Memoised on (nodes, rpsMetrics) so the heavy
// reducer runs only when the SSE delivers new data.
export function useClusterAggregates(): ClusterAggregates {
  const nodes = useClusterStore((s) => s.nodes)
  const rpsMetrics = useClusterStore((s) => s.clusterRpsMetrics)
  return useMemo(() => computeClusterAggregates(nodes, rpsMetrics), [nodes, rpsMetrics])
}

// useServicesActiveCount — count of services with at least one
// running copy. Single-number selector keeps the cluster-overview
// "Services" tile re-rendering only when this count actually moves.
export function useServicesActiveCount(): number {
  return useClusterStore((s) => s.services.filter((x) => (x.current_copies ?? 0) > 0).length)
}
