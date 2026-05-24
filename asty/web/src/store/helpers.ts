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

// stableMerge returns either `prev` (when `next` is structurally
// identical to it) or a new array that reuses prev's element
// references for unchanged items. Identity preservation is what lets
// React.memo / referential-equality selectors skip work on snapshots
// that repeat the same data — a backend tick that doesn't move
// anything costs zero re-renders downstream.
//
// Equality is shallow with one extra step: primitive-array fields
// (e.g. Node.processes) are compared element-by-element. Objects with
// nested non-array fields fall through as "different" — call sites
// pick the shape they own.
export function stableMerge<T>(prev: T[], next: T[], getKey: (x: T) => string): T[] {
  if (prev === next) return prev
  if (prev.length !== next.length) return mergeIndividually(prev, next, getKey)
  const prevByKey = new Map<string, T>()
  for (const p of prev) prevByKey.set(getKey(p), p)
  let dirty = false
  const out: T[] = new Array(next.length)
  for (let i = 0; i < next.length; i++) {
    const n = next[i]
    const p = prevByKey.get(getKey(n))
    if (p && shallowEqual(p, n)) {
      out[i] = p
    } else {
      out[i] = n
      dirty = true
    }
  }
  if (!dirty) {
    // Same set + same content — keep prev's reference only when the
    // order matches too, otherwise the keyed React tree wouldn't see
    // the move.
    for (let i = 0; i < prev.length; i++) {
      if (getKey(prev[i]) !== getKey(next[i])) return out
    }
    return prev
  }
  return out
}

function mergeIndividually<T>(prev: T[], next: T[], getKey: (x: T) => string): T[] {
  const prevByKey = new Map<string, T>()
  for (const p of prev) prevByKey.set(getKey(p), p)
  return next.map((n) => {
    const p = prevByKey.get(getKey(n))
    return p && shallowEqual(p, n) ? p : n
  })
}

function shallowEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if (typeof a !== 'object' || typeof b !== 'object' || a === null || b === null) return false
  const ka = Object.keys(a)
  if (ka.length !== Object.keys(b).length) return false
  const ao = a as Record<string, unknown>
  const bo = b as Record<string, unknown>
  for (const k of ka) {
    const av = ao[k]
    const bv = bo[k]
    if (av === bv) continue
    // One-level array-of-primitives check (Node.processes etc.)
    if (Array.isArray(av) && Array.isArray(bv) && av.length === bv.length) {
      let ok = true
      for (let i = 0; i < av.length; i++) {
        if (av[i] !== bv[i]) { ok = false; break }
      }
      if (ok) continue
    }
    return false
  }
  return true
}
