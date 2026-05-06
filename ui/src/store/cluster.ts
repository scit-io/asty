import { create } from 'zustand'
import type {
  Node,
  ClusterStatus,
  MetricPoint,
  Allocation,
  ServiceDefinition,
} from '@/types'

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
  cpuMetrics: MetricPoint[]
  memoryMetrics: MetricPoint[]
}

interface ClusterStore {
  // Cluster-level (populated by global SSE)
  clusterStatus: ClusterStatus | null
  nodes: Node[]
  services: ServiceDefinition[]

  // Detail caches (populated by per-page SSE subscriptions)
  nodeCache: Record<string, NodeData>
  serviceCache: Record<string, ServiceData>
  allocationCache: Record<string, AllocationData>

  sseConnected: boolean

  // Global SSE (cluster status / nodes / services / drain_progress)
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

export const useClusterStore = create<ClusterStore>((set, get) => ({
  clusterStatus: null,
  nodes: [],
  services: [],
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
            nodeCache[node.id] = existing
              ? { ...existing, node }
              : {
                  node,
                  allocations: [],
                  cpuMetrics: [],
                  memoryMetrics: [],
                  rpsMetrics: [],
                }
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

      es.addEventListener('drain_progress', (event) => {
        try {
          const data = JSON.parse(event.data)
          if (data.node_id && data.status && VALID_NODE_STATUSES.has(data.status)) {
            const cached = get().nodeCache[data.node_id]
            if (cached?.node) {
              set((state) => ({
                nodeCache: {
                  ...state.nodeCache,
                  [data.node_id]: {
                    ...cached,
                    node: { ...cached.node, status: data.status },
                  },
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
            const existing = state.nodeCache[nodeId] || {
              node: null,
              allocations: [],
              cpuMetrics: [],
              memoryMetrics: [],
              rpsMetrics: [],
            }
            return {
              nodeCache: {
                ...state.nodeCache,
                [nodeId]: { ...existing, allocations },
              },
            }
          })
        } catch { /* ignore */ }
      })

      es.addEventListener('metrics', (event) => {
        try {
          const data = JSON.parse(event.data)
          set((state) => {
            const existing = state.nodeCache[nodeId] || {
              node: null,
              allocations: [],
              cpuMetrics: [],
              memoryMetrics: [],
              rpsMetrics: [],
            }
            return {
              nodeCache: {
                ...state.nodeCache,
                [nodeId]: {
                  ...existing,
                  cpuMetrics: data.cpu || [],
                  memoryMetrics: data.memory || [],
                  rpsMetrics: data.rps || [],
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
            const existing = state.serviceCache[name] || {
              service: null,
              allocations: [],
              cpuMetrics: [],
              memoryMetrics: [],
              allocCountMetrics: [],
            }
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

      es.addEventListener('metrics', (event) => {
        try {
          const data = JSON.parse(event.data)
          set((state) => {
            const existing = state.serviceCache[name] || {
              service: null,
              allocations: [],
              cpuMetrics: [],
              memoryMetrics: [],
              allocCountMetrics: [],
            }
            return {
              serviceCache: {
                ...state.serviceCache,
                [name]: {
                  ...existing,
                  cpuMetrics: data.cpu || [],
                  memoryMetrics: data.memory || [],
                  allocCountMetrics: data.allocations_count || [],
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
          set((state) => {
            const existing = state.allocationCache[allocId] || {
              allocation: null,
              cpuMetrics: [],
              memoryMetrics: [],
            }
            return {
              allocationCache: {
                ...state.allocationCache,
                [allocId]: { ...existing, allocation: data.allocation || null },
              },
            }
          })
        } catch { /* ignore */ }
      })

      es.addEventListener('metrics', (event) => {
        try {
          const data = JSON.parse(event.data)
          set((state) => {
            const existing = state.allocationCache[allocId] || {
              allocation: null,
              cpuMetrics: [],
              memoryMetrics: [],
            }
            return {
              allocationCache: {
                ...state.allocationCache,
                [allocId]: {
                  ...existing,
                  cpuMetrics: data.cpu || [],
                  memoryMetrics: data.memory || [],
                },
              },
            }
          })
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
          [nodeId]: {
            ...cached,
            node: { ...cached.node, status },
          },
        },
      }
    })
  },
}))
