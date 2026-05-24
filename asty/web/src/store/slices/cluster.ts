import { apiPaths } from '@/lib/routes'
import { openStream, type ConnectionState } from '@/store/stream'
import { appendMetrics, stableMerge, VALID_NODE_STATUSES } from '../helpers'
import type { ClusterStore, Scheduler, SliceDeps } from '../types'
import type { Node } from '@/types'

// makeStatusHandler builds the common 'status'-event listener every
// stream's setup invokes. It lives in the cluster slice because the
// field it mutates (clusterStatus) is owned here — other slices'
// subscribe* fns receive the produced handler via SliceDeps.
export function makeStatusHandler(scheduleSet: Scheduler) {
  return (es: EventSource) => {
    es.addEventListener('status', (event) => {
      try {
        const data = JSON.parse((event as MessageEvent).data)
        scheduleSet(() => ({
          clusterStatus: {
            cluster: data.cluster,
            services: data.services || { loaded: 0 },
          },
        }))
      } catch { /* ignore */ }
    })
  }
}

// Cluster slice — owns the cluster overview snapshot (clusterStatus +
// cluster-wide CPU/Memory/RPS timeseries) and the single SSE that
// feeds the / route. Touches nodes / services / clusterStatus across
// the slice boundary via the shared scheduleSet — those fields live
// in other slices but every per-resource stream emits the cross-slice
// updates so the Header stays warm regardless of the active page.
export function createClusterSlice({ set, scheduleSet, attachStatusHandler }: SliceDeps): Pick<
  ClusterStore,
  'clusterStatus' | 'clusterCpuMetrics' | 'clusterMemoryMetrics' | 'clusterRpsMetrics' | 'subscribeCluster'
> {
  return {
    clusterStatus: null,
    clusterCpuMetrics: [],
    clusterMemoryMetrics: [],
    clusterRpsMetrics: [],

    subscribeCluster: () => {
      const onState = (st: ConnectionState) => {
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
      const close = openStream(apiPaths.cluster, (es) => {
        attachStatusHandler(es)
        es.addEventListener('nodes', (event) => {
          try {
            const data = JSON.parse((event as MessageEvent).data)
            const incoming: Node[] = data.nodes || []
            scheduleSet((state) => ({ nodes: stableMerge(state.nodes, incoming, (n) => n.id) }))
          } catch { /* ignore */ }
        })
        es.addEventListener('services', (event) => {
          try {
            const data = JSON.parse((event as MessageEvent).data)
            scheduleSet(() => ({ services: data.services || [] }))
          } catch { /* ignore */ }
        })
        es.addEventListener('cluster_metrics', (event) => {
          try {
            const data = JSON.parse((event as MessageEvent).data)
            scheduleSet((state) => ({
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
              scheduleSet((state) => ({
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
  }
}
