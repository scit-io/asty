import { create } from 'zustand'
import type {
  Node,
  ClusterStatus,
  MetricPoint,
  Allocation,
  ServiceDefinition,
} from '@/types'

// Max chart points kept in memory per series (2h at 5s = 1440 ticks,
// but metricsStore records at hub interval so ~1440 points max).
const MAX_CHART_POINTS = 1440

function appendMetrics(existing: MetricPoint[], incoming: MetricPoint[]): MetricPoint[] {
  if (!incoming.length) return existing
  const merged = [...existing, ...incoming]
  return merged.length > MAX_CHART_POINTS ? merged.slice(merged.length - MAX_CHART_POINTS) : merged
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
}

interface ClusterStore {
  // Cluster-level (populated by global SSE)
  clusterStatus: ClusterStatus | null
  nodes: Node[]
  services: ServiceDefinition[]
  clusterCpuMetrics: MetricPoint[]
  clusterMemoryMetrics: MetricPoint[]
  clusterRpsMetrics: MetricPoint[]

  // Detail caches (populated by per-page SSE subscriptions)
  nodeCache: Record<string, NodeData>
  serviceCache: Record<string, ServiceData>
  allocationCache: Record<string, AllocationData>

  sseConnected: boolean

  // Global SSE (cluster status / nodes / services / cluster_metrics / drain_progress)
  initSSE: () => () => void

  // Per-page SSE subscriptions — return unsubscribe fn
  subscribeNode: (nodeId: string) => () => void
  subscribeService: (name: string) => () => void
  subscribeAllocation: (allocId: string) => () => void

  // Optimistic mutation (used by drain action before SSE catches up)
  updateNodeStatus: (nodeId: string, status: Node['status']) => void
}

const VALID_NODE_STATUSES = new Set<Node['status']>([
  'ready', 'down', 'initializing', 'draining', 'drained',
])

// Reusable EventSource lifecycle with exponential backoff reconnect.
function openStream(
  url: string,
  setup: (es: EventSource) => void,
): () => void {
  let cancelled = false
  let retryCount = 0
  let retryTimer: ReturnType<typeof setTimeout> | null = null
  let es: EventSource | null = null

  const open = () => {
    if (cancelled) return
    es = new EventSource(url)
    setup(es)
    es.onopen = () => { retryCount = 0 }
    es.onerror = () => {
      es?.close()
      if (cancelled) return
      retryCount++
      if (retryCount > 10) return
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

export const useClusterStore = create<ClusterStore>((set, get) => ({
  clusterStatus: null,
  nodes: [],
  services: [],
  clusterCpuMetrics: [],
  clusterMemoryMetrics: [],
  clusterRpsMetrics: [],
  nodeCache: {},
  serviceCache: {},
  allocationCache: {},
  sseConnected: false,

  initSSE: () => {
    if (get().sseConnected) return () => {}
    set({ sseConnected: true })

    const close = openStream('/api/v1/stream', (es) => {
      es.addEventListener('status', (event) => {
        try {
          const data = JSON.parse(event.data)
          set({
            clusterStatus: {
              cluster: data.cluster,
              services: data.services || { loaded: 0 },
            },
          })
        } catch { /* ignore */ }
      })

      es.addEventListener('nodes', (event) => {
        try {
          const data = JSON.parse(event.data)
          const nodes: Node[] = data.nodes || []
          const nodeCache = { ...get().nodeCache }
          for (const node of nodes) {
            const existing = nodeCache[node.id]
            nodeCache[node.id] = existing ? { ...existing, node } : { ...emptyNodeData(), node }
          }
          set({ nodes, nodeCache })
        } catch { /* ignore */ }
      })

      es.addEventListener('services', (event) => {
        try {
          const data = JSON.parse(event.data)
          set({ services: data.services || [] })
        } catch { /* ignore */ }
      })

      es.addEventListener('cluster_metrics', (event) => {
        try {
          const data = JSON.parse(event.data)
          set((state) => ({
            clusterCpuMetrics: appendMetrics(state.clusterCpuMetrics, data.cpu || []),
            clusterMemoryMetrics: appendMetrics(state.clusterMemoryMetrics, data.memory || []),
            clusterRpsMetrics: appendMetrics(state.clusterRpsMetrics, data.rps || []),
          }))
        } catch { /* ignore */ }
      })

      es.addEventListener('drain_progress', (event) => {
        try {
          const data = JSON.parse(event.data)
          if (data.node_id && data.status && VALID_NODE_STATUSES.has(data.status)) {
            const cached = get().nodeCache[data.node_id]
            if (cached?.node) {
              set((state) => ({
                nodeCache: {
                  ...state.nodeCache,
                  [data.node_id]: { ...cached, node: { ...cached.node, status: data.status } },
                },
              }))
            }
          }
        } catch { /* ignore */ }
      })
    })

    return () => {
      close()
      set({ sseConnected: false })
    }
  },

  subscribeNode: (nodeId) => {
    return openStream(`/api/v1/stream/node/${nodeId}`, (es) => {
      es.addEventListener('allocations', (event) => {
        try {
          const data = JSON.parse(event.data)
          const allocations: Allocation[] = data.allocations || []
          set((state) => {
            const existing = state.nodeCache[nodeId] || emptyNodeData()
            return { nodeCache: { ...state.nodeCache, [nodeId]: { ...existing, allocations } } }
          })
        } catch { /* ignore */ }
      })

      // Delta append: server sends only new points since last tick.
      es.addEventListener('metrics', (event) => {
        try {
          const data = JSON.parse(event.data)
          set((state) => {
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
    })
  },

  subscribeService: (name) => {
    return openStream(`/api/v1/stream/service/${name}`, (es) => {
      es.addEventListener('detail', (event) => {
        try {
          const data = JSON.parse(event.data)
          set((state) => {
            const existing = state.serviceCache[name] || emptyServiceData()
            return {
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

      // Delta append for service metrics.
      es.addEventListener('metrics', (event) => {
        try {
          const data = JSON.parse(event.data)
          set((state) => {
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
    })
  },

  subscribeAllocation: (allocId) => {
    return openStream(`/api/v1/stream/allocation/${allocId}`, (es) => {
      es.addEventListener('detail', (event) => {
        try {
          const data = JSON.parse(event.data)
          set((state) => ({
            allocationCache: {
              ...state.allocationCache,
              [allocId]: { allocation: data.allocation || null },
            },
          }))
        } catch { /* ignore */ }
      })
    })
  },

  updateNodeStatus: (nodeId, status) => {
    set((state) => {
      const cached = state.nodeCache[nodeId]
      if (!cached?.node) return state
      return {
        nodeCache: {
          ...state.nodeCache,
          [nodeId]: { ...cached, node: { ...cached.node, status } },
        },
      }
    })
  },
}))
