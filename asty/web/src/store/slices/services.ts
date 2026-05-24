import { api } from '@/api/client'
import { apiPaths } from '@/lib/routes'
import { openStream, type ConnectionState } from '@/store/stream'
import type { DeploymentsResponse } from '@/types'
import { appendMetrics, emptyServiceData } from '../helpers'
import type { ClusterStore, SliceDeps } from '../types'
import {
  startAutoscalerPoller,
  subscribeDeployProgress,
  writeAutoscalerToCache,
  writeHistoryToCache,
  type AutoscalerPayload,
} from './service-streams'

// Services slice — owns the services list and per-service cache.
// subscribeService is the heaviest of the subscribe* fns: opens the
// main SSE, an autoscaler REST poller, a one-shot history load, a
// versions fetch, and a separate deploy-progress SSE wired through
// the same openStream so it shares the reconnect lifecycle. The
// poller and deploy SSE live in ./service-streams.ts to keep this
// file under the 200-LOC cap.
export function createServicesSlice({ set, scheduleSet, attachStatusHandler }: SliceDeps): Pick<
  ClusterStore,
  'services' | 'serviceCache' | 'subscribeServices' | 'subscribeService' | 'refreshService'
> {
  return {
    services: [],
    serviceCache: {},

    subscribeServices: () => {
      const onState = (st: ConnectionState) => {
        if (st !== 'streaming') {
          set(() => ({ services: [], clusterStatus: null }))
        }
      }
      const close = openStream(apiPaths.services, (es) => {
        attachStatusHandler(es)
        es.addEventListener('services', (event) => {
          try {
            const data = JSON.parse((event as MessageEvent).data)
            scheduleSet(() => ({ services: data.services || [] }))
          } catch { /* ignore */ }
        })
      }, onState)
      return close
    },

    subscribeService: (name) => {
      const onState = (st: ConnectionState) => {
        // Only wipe on terminal "dead" — transient 'connecting' fires
        // on every tab switch (each page reopens its EventSource) and
        // would clear cached service/allocations between mount and the
        // first SSE frame, making the type Badge in the header flicker.
        if (st === 'dead') {
          set((state) => ({
            serviceCache: { ...state.serviceCache, [name]: emptyServiceData() },
          }))
        }
      }
      const close = openStream(apiPaths.service(name), (es) => {
        attachStatusHandler(es)
        es.addEventListener('detail', (event) => {
          try {
            const data = JSON.parse((event as MessageEvent).data)
            scheduleSet((state) => {
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
            scheduleSet((state) => {
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

      const stopAutoscaler = startAutoscalerPoller(name, set)

      // Deploy history initial load + reused by deploy SSE on dead /
      // on terminal progress event. historyCancelled flips on cleanup
      // so a late response doesn't write into the wiped cache slot.
      let historyCancelled = false
      const loadHistory = async () => {
        try {
          const res = await api.getServiceDeployments(name) as DeploymentsResponse
          if (historyCancelled) return
          writeHistoryToCache(set, name, res.deployments ?? [])
        } catch { /* keep current */ }
      }
      loadHistory()

      // One-shot versions fetch — list rarely moves; the operator gets
      // the freshest copy by reopening the page.
      api.getServiceVersions(name).then((res) => {
        if (historyCancelled) return
        set((state) => {
          const existing = state.serviceCache[name] || emptyServiceData()
          return {
            serviceCache: {
              ...state.serviceCache,
              [name]: { ...existing, availableVersions: res.versions ?? [] },
            },
          }
        })
      }).catch(() => { /* keep current */ })

      const closeDeploy = subscribeDeployProgress(name, set, loadHistory)

      return () => {
        close()
        stopAutoscaler()
        historyCancelled = true
        closeDeploy()
        // Same rule as subscribeNode — only the chart timeseries gets
        // freed; the service snapshot + autoscaler + deploy caches stay
        // for the next page open so re-entry is instant.
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

    refreshService: async (name) => {
      // Fire the same two writes subscribeService runs on a timer.
      // Parallel — independent endpoints. Errors swallowed so a
      // transient hiccup doesn't surface as a toast on a successful
      // mutation.
      const writeAutoscaler = api.getServiceAutoscaler(name)
        .then((res) => writeAutoscalerToCache(set, name, res as AutoscalerPayload))
        .catch(() => { /* keep current */ })
      const writeHistory = api.getServiceDeployments(name)
        .then((res) => writeHistoryToCache(set, name, (res as DeploymentsResponse).deployments ?? []))
        .catch(() => { /* keep current */ })
      await Promise.all([writeAutoscaler, writeHistory])
    },
  }
}
