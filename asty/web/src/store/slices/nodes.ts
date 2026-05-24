import { apiPaths } from '@/lib/routes'
import { openStream, type ConnectionState } from '@/store/stream'
import { appendMetrics, emptyNodeData } from '../helpers'
import type { ClusterStore, SliceDeps } from '../types'

// Nodes slice — owns the nodes list and the per-node cache. Provides
// two subscribe* fns: list (subscribeNodes) and detail (subscribeNode),
// plus updateNodeStatus for optimistic UI on drain.
export function createNodesSlice({ set, scheduleSet, attachStatusHandler }: SliceDeps): Pick<
  ClusterStore,
  'nodes' | 'nodeCache' | 'subscribeNodes' | 'subscribeNode' | 'updateNodeStatus'
> {
  return {
    nodes: [],
    nodeCache: {},

    subscribeNodes: () => {
      const onState = (st: ConnectionState) => {
        if (st !== 'streaming') {
          set(() => ({ nodes: [], clusterStatus: null }))
        }
      }
      const close = openStream(apiPaths.nodes, (es) => {
        attachStatusHandler(es)
        es.addEventListener('nodes', (event) => {
          try {
            const data = JSON.parse((event as MessageEvent).data)
            scheduleSet(() => ({ nodes: data.nodes || [] }))
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

      const onState = (st: ConnectionState) => {
        // Only wipe on terminal "dead" — transient 'connecting' fires
        // on every tab switch (each page reopens its EventSource) and
        // would clear the cached node snapshot between mount and the
        // first SSE frame, making the header (id + status dot + ip/dc
        // line) flicker. 'reconnecting' similarly keeps the last good
        // snapshot until the new stream lands.
        if (st === 'dead') {
          set((state) => ({
            nodeCache: { ...state.nodeCache, [nodeId]: emptyNodeData() },
          }))
        }
      }
      const close = openStream(apiPaths.node(nodeId), (es) => {
        attachStatusHandler(es)
        es.addEventListener('node', (event) => {
          try {
            const data = JSON.parse((event as MessageEvent).data)
            if (!data.node) return
            scheduleSet((state) => {
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
            scheduleSet(() => ({ services: data.services || [] }))
          } catch { /* ignore */ }
        })
        es.addEventListener('allocations', (event) => {
          try {
            const data = JSON.parse((event as MessageEvent).data)
            scheduleSet((state) => {
              const existing = state.nodeCache[nodeId] || emptyNodeData()
              return { nodeCache: { ...state.nodeCache, [nodeId]: { ...existing, allocations: data.allocations || [] } } }
            })
          } catch { /* ignore */ }
        })
        es.addEventListener('metrics', (event) => {
          try {
            const data = JSON.parse((event as MessageEvent).data)
            scheduleSet((state) => {
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
  }
}
