import { apiPaths } from '@/lib/routes'
import { openStream, type ConnectionState } from '@/store/stream'
import { appendMetrics, emptyAllocationData } from '../helpers'
import type { ClusterStore, SliceDeps } from '../types'

// Allocations slice — owns the per-allocation cache. One subscribe*
// fn (subscribeAllocation) opens the per-resource SSE; charts get
// dropped on unmount, the snapshot stays for instant re-entry.
export function createAllocationsSlice({ set, scheduleSet, attachStatusHandler }: SliceDeps): Pick<
  ClusterStore,
  'allocationCache' | 'subscribeAllocation'
> {
  return {
    allocationCache: {},

    subscribeAllocation: (nodeId, allocId) => {
      const onState = (st: ConnectionState) => {
        // Match subscribeNode: only wipe on terminal "dead" so transient
        // 'connecting'/'reconnecting' don't blank the charts mid-session.
        if (st === 'dead') {
          set((state) => ({
            allocationCache: { ...state.allocationCache, [allocId]: emptyAllocationData() },
          }))
        }
      }
      const close = openStream(apiPaths.allocation(nodeId, allocId), (es) => {
        attachStatusHandler(es)
        es.addEventListener('detail', (event) => {
          try {
            const data = JSON.parse((event as MessageEvent).data)
            scheduleSet((state) => {
              const existing = state.allocationCache[allocId] || emptyAllocationData()
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
            scheduleSet((state) => {
              const existing = state.allocationCache[allocId] || emptyAllocationData()
              return {
                allocationCache: {
                  ...state.allocationCache,
                  [allocId]: { ...existing, service: data.service },
                },
              }
            })
          } catch { /* ignore */ }
        })
        es.addEventListener('metrics', (event) => {
          try {
            const data = JSON.parse((event as MessageEvent).data)
            scheduleSet((state) => {
              const existing = state.allocationCache[allocId] || emptyAllocationData()
              return {
                allocationCache: {
                  ...state.allocationCache,
                  [allocId]: {
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
        // Drop chart timeseries on unmount — only the visible page reads
        // them; next visit rebuilds from the stream's initial frames.
        set((state) => {
          const existing = state.allocationCache[allocId]
          if (!existing) return {}
          return {
            allocationCache: {
              ...state.allocationCache,
              [allocId]: { ...existing, cpuMetrics: [], memoryMetrics: [], rpsMetrics: [] },
            },
          }
        })
      }
    },
  }
}
