import { api } from '@/api/client'
import { AUTOSCALER_POLL_MS } from '@/lib/constants'
import { apiPaths } from '@/lib/routes'
import { openStream, type ConnectionState } from '@/store/stream'
import type { DeploymentRecord, ScalingEvent } from '@/types'
import { emptyServiceData } from '../helpers'
import type { Setter } from '../types'

// Shape of the /autoscaler payload — repeated at every callsite so
// kept local. Out-of-band typing: api.getServiceAutoscaler returns
// `unknown` (the response type isn't formally declared).
export type AutoscalerPayload = {
  events?: ScalingEvent[]
  min_copies: number
  min_copies_default: number
  min_copies_override: boolean
  max_copies: number
  deploy_in_progress: boolean
}

// writeAutoscalerToCache lifts the cache mutation out of the two
// callsites (polling tick + manual refresh) — same shape, same
// targets, same fallback to emptyServiceData when no prior cache.
export function writeAutoscalerToCache(set: Setter, name: string, res: AutoscalerPayload) {
  set((state) => {
    const existing = state.serviceCache[name] || emptyServiceData()
    return {
      serviceCache: {
        ...state.serviceCache,
        [name]: {
          ...existing,
          autoscaler: {
            min_copies: res.min_copies,
            min_copies_default: res.min_copies_default,
            min_copies_override: res.min_copies_override,
            max_copies: res.max_copies,
            deploy_in_progress: res.deploy_in_progress,
          },
          scalingEvents: res.events ?? [],
        },
      },
    }
  })
}

export function writeHistoryToCache(set: Setter, name: string, deployments: DeploymentRecord[]) {
  set((state) => {
    const existing = state.serviceCache[name] || emptyServiceData()
    return {
      serviceCache: {
        ...state.serviceCache,
        [name]: { ...existing, deployHistory: deployments },
      },
    }
  })
}

// startAutoscalerPoller kicks off the /autoscaler REST poll and
// returns a cleanup that cancels the in-flight timer + flags
// pending async writes as cancelled. The poller is the single
// source for both Overview's configuration card and the Scaling
// events tab.
export function startAutoscalerPoller(name: string, set: Setter): () => void {
  let timer: ReturnType<typeof setTimeout> | null = null
  let cancelled = false
  const poll = async () => {
    try {
      const res = await api.getServiceAutoscaler(name) as AutoscalerPayload
      if (cancelled) return
      writeAutoscalerToCache(set, name, res)
    } catch { /* keep current */ }
    if (!cancelled) timer = setTimeout(poll, AUTOSCALER_POLL_MS)
  }
  poll()
  return () => {
    cancelled = true
    if (timer) clearTimeout(timer)
  }
}

// subscribeDeployProgress opens the per-service deploy-progress SSE
// and returns a cleanup. On terminal 'dead' it clears liveDeploy so
// a frozen "running" pill can't outlive a long outage, then
// refetches history once via the supplied closure to catch the
// deploy's terminal state if it landed while we were disconnected.
export function subscribeDeployProgress(
  name: string,
  set: Setter,
  loadHistory: () => Promise<void>,
): () => void {
  const onDeployState = (st: ConnectionState) => {
    if (st !== 'dead') return
    set((state) => {
      const existing = state.serviceCache[name]
      if (!existing) return {}
      return {
        serviceCache: {
          ...state.serviceCache,
          [name]: { ...existing, liveDeploy: null },
        },
      }
    })
    loadHistory()
  }
  return openStream(apiPaths.serviceDeploy(name), (es) => {
    es.addEventListener('progress', (event) => {
      try {
        const rec = JSON.parse((event as MessageEvent).data) as DeploymentRecord
        set((state) => {
          const existing = state.serviceCache[name] || emptyServiceData()
          return {
            serviceCache: {
              ...state.serviceCache,
              [name]: { ...existing, liveDeploy: rec.status === 'running' ? rec : null },
            },
          }
        })
        if (rec.status !== 'running') loadHistory()
      } catch { /* ignore malformed */ }
    })
  }, onDeployState)
}
