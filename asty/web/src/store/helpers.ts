import { MAX_CHART_POINTS } from '@/lib/constants'
import type { MetricPoint, Node } from '@/types'
import type { NodeData, ServiceData, AllocationData } from './types'

// appendMetrics merges the latest server-sent points into the current
// in-memory series, trimming to MAX_CHART_POINTS so the buffer stays
// bounded. Empty incoming = no-op; empty existing = take incoming
// tail.
export function appendMetrics(existing: MetricPoint[], incoming: MetricPoint[]): MetricPoint[] {
  if (!incoming.length) return existing
  if (!existing.length) return incoming.slice(-MAX_CHART_POINTS)
  const merged = existing.concat(incoming)
  if (merged.length > MAX_CHART_POINTS) {
    return merged.slice(merged.length - MAX_CHART_POINTS)
  }
  return merged
}

// Validated node statuses propagated from the drain_progress SSE
// event. Defensive — the wire could theoretically deliver anything,
// and a typo'd literal would break the union type silently.
export const VALID_NODE_STATUSES = new Set<Node['status']>([
  'ready', 'down', 'draining', 'drained', 'paused',
])

export const emptyNodeData = (): NodeData => ({
  node: null, allocations: [], cpuMetrics: [], memoryMetrics: [], rpsMetrics: [],
})

export const emptyServiceData = (): ServiceData => ({
  service: null, allocations: [], cpuMetrics: [], memoryMetrics: [], allocCountMetrics: [],
  autoscaler: null, scalingEvents: [], liveDeploy: null, deployHistory: [], availableVersions: [],
})

export const emptyAllocationData = (): AllocationData => ({
  allocation: null, service: null, cpuMetrics: [], memoryMetrics: [], rpsMetrics: [],
})
