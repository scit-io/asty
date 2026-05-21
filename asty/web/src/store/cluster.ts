import { create } from 'zustand'
import { API_PREFIX } from '@/api/client'
import type {
  Node,
  ClusterStatus,
  MetricPoint,
  Allocation,
  ServiceDefinition,
} from '@/types'

// Max chart points kept in memory per series (5min at 5s = 60 points).
const MAX_CHART_POINTS = 60

// STORE_FLUSH_MS bounds how often SSE-driven updates land in zustand.
// The backend already debounces snapshots at 500 ms, but a single
// snapshot fans out as multiple event types ('status', 'nodes',
// 'services', 'metrics', …) — without batching those would be N
// separate setState calls and N re-renders per snapshot. 100 ms keeps
// updates feeling live (10 fps) while collapsing each snapshot into
// one render burst regardless of how many event listeners fire for it.
const STORE_FLUSH_MS = 100

function appendMetrics(existing: MetricPoint[], incoming: MetricPoint[]): MetricPoint[] {
  if (!incoming.length) return existing
  if (!existing.length) return incoming.slice(-MAX_CHART_POINTS)
  const merged = existing.concat(incoming)
  if (merged.length > MAX_CHART_POINTS) {
    return merged.slice(merged.length - MAX_CHART_POINTS)
  }
  return merged
}

interface NodeData {
  node: Node | null
  allocations: Allocation[]
  cpuMetrics: MetricPoint[]
  memoryMetrics: MetricPoint[]
  rpsMetrics: MetricPoint[]
}

interface ServiceData {
  service: ServiceDefinition | null
  allocations: Allocation[]
  cpuMetrics: MetricPoint[]
  memoryMetrics: MetricPoint[]
  allocCountMetrics: MetricPoint[]
}

interface AllocationData {
  allocation: Allocation | null
  service: ServiceDefinition | null
}

interface ClusterStore {
  // Globals — populated by `status` / `services` events that every
  // per-resource stream emits, so any active page keeps these warm.
  clusterStatus: ClusterStatus | null
  nodes: Node[]
  services: ServiceDefinition[]

  // Cluster overview timeseries (only populated by subscribeCluster).
  clusterCpuMetrics: MetricPoint[]
  clusterMemoryMetrics: MetricPoint[]
  clusterRpsMetrics: MetricPoint[]

  // Per-resource page caches.
  nodeCache: Record<string, NodeData>
  serviceCache: Record<string, ServiceData>
  allocationCache: Record<string, AllocationData>

  // Page subscriptions — each opens exactly ONE EventSource for its
  // page. Cluster status, services list, nodes list are folded into
  // every per-resource stream so the Header stays live without a
  // parallel global subscription.
  subscribeCluster: () => () => void
  subscribeNodes: () => () => void
  subscribeServices: () => () => void
  subscribeNode: (nodeId: string) => () => void
  subscribeService: (name: string) => () => void
  subscribeAllocation: (nodeId: string, allocId: string) => () => void

  // Optimistic mutation (used by drain before SSE catches up).
  updateNodeStatus: (nodeId: string, status: Node['status']) => void
}

const VALID_NODE_STATUSES = new Set<Node['status']>([
  'ready', 'down', 'draining', 'drained', 'paused',
])

// connectionState — observable lifecycle of one SSE subscription. Not
// exported because the store no longer surfaces it: the only consumer
// is the local onState hook each subscribeXxx uses to wipe cached
// snapshots when the live feed drops (so the UI never paints stale
// values).
type connectionState = 'connecting' | 'streaming' | 'reconnecting' | 'dead'

// Max reconnect attempts before the stream is declared dead. With the
// exponential backoff below (max 60 s), 10 attempts cover ~10 minutes
// of outage — past that we stop hammering the server.
const STREAM_MAX_RETRIES = 10

// Reusable EventSource lifecycle with exponential backoff reconnect.
// onState fires on every transition so the store can mirror the
// connection status into a UI-visible field.
function openStream(
  url: string,
  setup: (es: EventSource) => void,
  onState?: (state: connectionState) => void,
): () => void {
  let cancelled = false
  let retryCount = 0
  let retryTimer: ReturnType<typeof setTimeout> | null = null
  let es: EventSource | null = null

  const open = () => {
    if (cancelled) return
    onState?.(retryCount === 0 ? 'connecting' : 'reconnecting')
    es = new EventSource(url)
    setup(es)
    es.onopen = () => {
      retryCount = 0
      onState?.('streaming')
    }
    es.onerror = () => {
      es?.close()
      if (cancelled) return
      retryCount++
      if (retryCount > STREAM_MAX_RETRIES) {
        onState?.('dead')
        return
      }
      onState?.('reconnecting')
      retryTimer = setTimeout(open, Math.min(3000 * Math.pow(2, retryCount - 1), 60000))
    }
  }

  open()
  return () => {
    cancelled = true
    if (retryTimer) clearTimeout(retryTimer)
    es?.close()
  }
}

const emptyNodeData = (): NodeData => ({
  node: null, allocations: [], cpuMetrics: [], memoryMetrics: [], rpsMetrics: [],
})

const emptyServiceData = (): ServiceData => ({
  service: null, allocations: [], cpuMetrics: [], memoryMetrics: [], allocCountMetrics: [],
})

// Common handler for the compact `status` event that every stream
// emits. Header reads clusterStatus from the store regardless of
// which page is open.
function attachStatusHandler(es: EventSource, set: SetFn) {
  es.addEventListener('status', (event) => {
    try {
      const data = JSON.parse((event as MessageEvent).data)
      scheduleSet(set, () => ({
        clusterStatus: {
          cluster: data.cluster,
          services: data.services || { loaded: 0 },
        },
      }))
    } catch { /* ignore */ }
  })
}

type SetFn = (fn: (s: ClusterStore) => Partial<ClusterStore>) => void

// Pending functional updates and a one-shot timer that flushes them in
// a single zustand `set` call. Each per-event listener feeds an update
// into pendingUpdates instead of calling `set` directly — see
// scheduleSet below.
let pendingUpdates: Array<(s: ClusterStore) => Partial<ClusterStore>> = []
let flushTimer: ReturnType<typeof setTimeout> | null = null

// scheduleSet collapses N updates within STORE_FLUSH_MS into one
// `set` so React renders the page once per snapshot, not once per
// event listener. The update functions still receive the latest
// running state, so reducers that read+merge (e.g. appendMetrics)
// stay correct.
function scheduleSet(set: (fn: (s: ClusterStore) => Partial<ClusterStore>) => void, fn: (s: ClusterStore) => Partial<ClusterStore>) {
  pendingUpdates.push(fn)
  if (flushTimer !== null) return
  flushTimer = setTimeout(() => {
    flushTimer = null
    const fns = pendingUpdates
    pendingUpdates = []
    set((current) => {
      let next: ClusterStore = current
      for (const fn of fns) {
        next = { ...next, ...fn(next) }
      }
      return next
    })
  }, STORE_FLUSH_MS)
}

export const useClusterStore = create<ClusterStore>((set) => ({
  clusterStatus: null,
  nodes: [],
  services: [],
  clusterCpuMetrics: [],
  clusterMemoryMetrics: [],
  clusterRpsMetrics: [],
  nodeCache: {},
  serviceCache: {},
  allocationCache: {},

  subscribeCluster: () => {
    const onState = (st: connectionState) => {
      // Leaving 'streaming' = lost the live feed. Wipe everything this
      // subscription owns: shared snapshot (nodes, services,
      // clusterStatus) and the cluster timeseries. Tiles fall back to
      // their empty-state rendering — never a stale number.
      if (st !== 'streaming') {
        set(() => ({
          nodes: [],
          services: [],
          clusterStatus: null,
          clusterCpuMetrics: [],
          clusterMemoryMetrics: [],
          clusterRpsMetrics: [],
        }))
      }
    }
    const close = openStream(`${API_PREFIX}/`, (es) => {
      attachStatusHandler(es, set)
      es.addEventListener('nodes', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
          scheduleSet(set, () => ({ nodes: data.nodes || [] }))
        } catch { /* ignore */ }
      })
      es.addEventListener('services', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
          scheduleSet(set, () => ({ services: data.services || [] }))
        } catch { /* ignore */ }
      })
      es.addEventListener('cluster_metrics', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
          scheduleSet(set, (state) => ({
            clusterCpuMetrics: appendMetrics(state.clusterCpuMetrics, data.cpu || []),
            clusterMemoryMetrics: appendMetrics(state.clusterMemoryMetrics, data.memory || []),
            clusterRpsMetrics: appendMetrics(state.clusterRpsMetrics, data.rps || []),
          }))
        } catch { /* ignore */ }
      })
      es.addEventListener('drain_progress', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
          if (data.node_id && data.status && VALID_NODE_STATUSES.has(data.status)) {
            scheduleSet(set, (state) => ({
              nodes: state.nodes.map((n) => n.id === data.node_id ? { ...n, status: data.status } : n),
            }))
          }
        } catch { /* ignore */ }
      })
    }, onState)
    return () => {
      close()
      // Cleanup on page-unmount: drop the chart timeseries so the next
      // mount starts clean.
      set(() => ({
        clusterCpuMetrics: [], clusterMemoryMetrics: [], clusterRpsMetrics: [],
      }))
    }
  },

  subscribeNodes: () => {
    const onState = (st: connectionState) => {
      if (st !== 'streaming') {
        set(() => ({ nodes: [], clusterStatus: null }))
      }
    }
    const close = openStream(`${API_PREFIX}/nodes`, (es) => {
      attachStatusHandler(es, set)
      es.addEventListener('nodes', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
          scheduleSet(set, () => ({ nodes: data.nodes || [] }))
        } catch { /* ignore */ }
      })
    }, onState)
    return close
  },

  subscribeServices: () => {
    const onState = (st: connectionState) => {
      if (st !== 'streaming') {
        set(() => ({ services: [], clusterStatus: null }))
      }
    }
    const close = openStream(`${API_PREFIX}/services`, (es) => {
      attachStatusHandler(es, set)
      es.addEventListener('services', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
          scheduleSet(set, () => ({ services: data.services || [] }))
        } catch { /* ignore */ }
      })
    }, onState)
    return close
  },

  subscribeNode: (nodeId) => {
    // Seed from any nodes list that's already in memory so the
    // first paint isn't a Skeleton when navigating from /nodes.
    set((state) => {
      const existing = state.nodeCache[nodeId] || emptyNodeData()
      const seed = state.nodes.find((n) => n.id === nodeId) || existing.node
      return { nodeCache: { ...state.nodeCache, [nodeId]: { ...existing, node: seed } } }
    })

    const onState = (st: connectionState) => {
      // Stream lost — wipe this node's snapshot. Tiles fall back to
      // their natural empty-state rendering instead of showing the
      // last-known CPU / memory values that may no longer be true.
      if (st !== 'streaming') {
        set((state) => ({
          nodeCache: { ...state.nodeCache, [nodeId]: emptyNodeData() },
        }))
      }
    }
    const close = openStream(`${API_PREFIX}/nodes/${nodeId}`, (es) => {
      attachStatusHandler(es, set)
      es.addEventListener('node', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
          if (!data.node) return
          scheduleSet(set, (state) => {
            const existing = state.nodeCache[nodeId] || emptyNodeData()
            return {
              nodeCache: { ...state.nodeCache, [nodeId]: { ...existing, node: data.node } },
              nodes: state.nodes.some((n) => n.id === data.node.id)
                ? state.nodes.map((n) => n.id === data.node.id ? data.node : n)
                : state.nodes,
            }
          })
        } catch { /* ignore */ }
      })
      es.addEventListener('services', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
          scheduleSet(set, () => ({ services: data.services || [] }))
        } catch { /* ignore */ }
      })
      es.addEventListener('allocations', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
          scheduleSet(set, (state) => {
            const existing = state.nodeCache[nodeId] || emptyNodeData()
            return { nodeCache: { ...state.nodeCache, [nodeId]: { ...existing, allocations: data.allocations || [] } } }
          })
        } catch { /* ignore */ }
      })
      es.addEventListener('metrics', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
          scheduleSet(set, (state) => {
            const existing = state.nodeCache[nodeId] || emptyNodeData()
            return {
              nodeCache: {
                ...state.nodeCache,
                [nodeId]: {
                  ...existing,
                  cpuMetrics: appendMetrics(existing.cpuMetrics, data.cpu || []),
                  memoryMetrics: appendMetrics(existing.memoryMetrics, data.memory || []),
                  rpsMetrics: appendMetrics(existing.rpsMetrics, data.rps || []),
                },
              },
            }
          })
        } catch { /* ignore */ }
      })
    }, onState)
    return () => {
      close()
      // Keep the node card (snapshot) but drop the chart timeseries —
      // only the visible page consumes them. Next visit rebuilds via
      // the stream's initial frames.
      set((state) => {
        const existing = state.nodeCache[nodeId]
        if (!existing) return {}
        return {
          nodeCache: {
            ...state.nodeCache,
            [nodeId]: { ...existing, cpuMetrics: [], memoryMetrics: [], rpsMetrics: [] },
          },
        }
      })
    }
  },

  subscribeService: (name) => {
    const onState = (st: connectionState) => {
      if (st !== 'streaming') {
        set((state) => ({
          serviceCache: { ...state.serviceCache, [name]: emptyServiceData() },
        }))
      }
    }
    const close = openStream(`${API_PREFIX}/services/${name}`, (es) => {
      attachStatusHandler(es, set)
      es.addEventListener('detail', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
          scheduleSet(set, (state) => {
            const existing = state.serviceCache[name] || emptyServiceData()
            // Mirror the service into the shared services list so
            // pages reading state.services (e.g. Header dropdowns)
            // see the runtime fields even without subscribeServices.
            const updatedServices = data.service
              ? (state.services.some((s) => s.Name === name)
                ? state.services.map((s) => s.Name === name ? data.service : s)
                : state.services.concat(data.service))
              : state.services
            return {
              services: updatedServices,
              serviceCache: {
                ...state.serviceCache,
                [name]: {
                  ...existing,
                  service: data.service || null,
                  allocations: data.allocations || [],
                },
              },
            }
          })
        } catch { /* ignore */ }
      })
      es.addEventListener('metrics', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
          scheduleSet(set, (state) => {
            const existing = state.serviceCache[name] || emptyServiceData()
            return {
              serviceCache: {
                ...state.serviceCache,
                [name]: {
                  ...existing,
                  cpuMetrics: appendMetrics(existing.cpuMetrics, data.cpu || []),
                  memoryMetrics: appendMetrics(existing.memoryMetrics, data.memory || []),
                  allocCountMetrics: appendMetrics(existing.allocCountMetrics, data.allocations_count || []),
                },
              },
            }
          })
        } catch { /* ignore */ }
      })
    }, onState)
    return () => {
      close()
      // Same rule as subscribeNode — only the chart timeseries gets
      // freed; the service snapshot stays for the next page open.
      set((state) => {
        const existing = state.serviceCache[name]
        if (!existing) return {}
        return {
          serviceCache: {
            ...state.serviceCache,
            [name]: { ...existing, cpuMetrics: [], memoryMetrics: [], allocCountMetrics: [] },
          },
        }
      })
    }
  },

  subscribeAllocation: (nodeId, allocId) => {
    const onState = (st: connectionState) => {
      if (st !== 'streaming') {
        set((state) => ({
          allocationCache: { ...state.allocationCache, [allocId]: { allocation: null, service: null } },
        }))
      }
    }
    const close = openStream(`${API_PREFIX}/nodes/${nodeId}/allocations/${allocId}`, (es) => {
      attachStatusHandler(es, set)
      es.addEventListener('detail', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
          scheduleSet(set, (state) => {
            const existing = state.allocationCache[allocId] || { allocation: null, service: null }
            return {
              allocationCache: {
                ...state.allocationCache,
                [allocId]: { ...existing, allocation: data.allocation || null },
              },
            }
          })
        } catch { /* ignore */ }
      })
      es.addEventListener('service', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
          if (!data.service) return
          scheduleSet(set, (state) => {
            const existing = state.allocationCache[allocId] || { allocation: null, service: null }
            return {
              allocationCache: {
                ...state.allocationCache,
                [allocId]: { ...existing, service: data.service },
              },
            }
          })
        } catch { /* ignore */ }
      })
    }, onState)
    return close
  },

  updateNodeStatus: (nodeId, status) => {
    set((state) => {
      const next: Partial<ClusterStore> = {}
      // Mirror in both the per-node cache and the shared nodes list.
      const cached = state.nodeCache[nodeId]
      if (cached?.node) {
        next.nodeCache = {
          ...state.nodeCache,
          [nodeId]: { ...cached, node: { ...cached.node, status } },
        }
      }
      if (state.nodes.some((n) => n.id === nodeId)) {
        next.nodes = state.nodes.map((n) => n.id === nodeId ? { ...n, status } : n)
      }
      return next
    })
  },
}))
