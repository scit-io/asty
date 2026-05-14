import { create } from 'zustand'
import { API_BASE } from '@/api/client'
import type {
  Node,
  ClusterStatus,
  MetricPoint,
  Allocation,
  ServiceDefinition,
} from '@/types'

// Max chart points kept in memory per series (5min at 5s = 60 points).
const MAX_CHART_POINTS = 60

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

// Common handler for the compact `status` event that every stream
// emits. Header reads clusterStatus from the store regardless of
// which page is open.
function attachStatusHandler(es: EventSource, set: (fn: (s: ClusterStore) => Partial<ClusterStore>) => void) {
  es.addEventListener('status', (event) => {
    try {
      const data = JSON.parse((event as MessageEvent).data)
      set(() => ({
        clusterStatus: {
          cluster: data.cluster,
          services: data.services || { loaded: 0 },
        },
      }))
    } catch { /* ignore */ }
  })
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
    return openStream(`${API_BASE}/`, (es) => {
      attachStatusHandler(es, set)
      es.addEventListener('nodes', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
          set(() => ({ nodes: data.nodes || [] }))
        } catch { /* ignore */ }
      })
      es.addEventListener('services', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
          set(() => ({ services: data.services || [] }))
        } catch { /* ignore */ }
      })
      es.addEventListener('cluster_metrics', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
          set((state) => ({
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
            set((state) => ({
              nodes: state.nodes.map((n) => n.id === data.node_id ? { ...n, status: data.status } : n),
            }))
          }
        } catch { /* ignore */ }
      })
    })
  },

  subscribeNodes: () => {
    return openStream(`${API_BASE}/nodes`, (es) => {
      attachStatusHandler(es, set)
      es.addEventListener('nodes', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
          set(() => ({ nodes: data.nodes || [] }))
        } catch { /* ignore */ }
      })
    })
  },

  subscribeServices: () => {
    return openStream(`${API_BASE}/services`, (es) => {
      attachStatusHandler(es, set)
      es.addEventListener('services', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
          set(() => ({ services: data.services || [] }))
        } catch { /* ignore */ }
      })
    })
  },

  subscribeNode: (nodeId) => {
    // Seed from any nodes list that's already in memory so the
    // first paint isn't a Skeleton when navigating from /nodes.
    set((state) => {
      const existing = state.nodeCache[nodeId] || emptyNodeData()
      const seed = state.nodes.find((n) => n.id === nodeId) || existing.node
      return { nodeCache: { ...state.nodeCache, [nodeId]: { ...existing, node: seed } } }
    })

    return openStream(`${API_BASE}/nodes/${nodeId}`, (es) => {
      attachStatusHandler(es, set)
      es.addEventListener('node', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
          if (!data.node) return
          set((state) => {
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
          set(() => ({ services: data.services || [] }))
        } catch { /* ignore */ }
      })
      es.addEventListener('allocations', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
          set((state) => {
            const existing = state.nodeCache[nodeId] || emptyNodeData()
            return { nodeCache: { ...state.nodeCache, [nodeId]: { ...existing, allocations: data.allocations || [] } } }
          })
        } catch { /* ignore */ }
      })
      es.addEventListener('metrics', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
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
    return openStream(`${API_BASE}/services/${name}`, (es) => {
      attachStatusHandler(es, set)
      es.addEventListener('detail', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
          set((state) => {
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

  subscribeAllocation: (nodeId, allocId) => {
    return openStream(`${API_BASE}/nodes/${nodeId}/allocations/${allocId}`, (es) => {
      attachStatusHandler(es, set)
      es.addEventListener('detail', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data)
          set((state) => {
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
          set((state) => {
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
    })
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
