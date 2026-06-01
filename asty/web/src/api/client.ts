import type {
  AllocationDetail,
  LogsResponse,
  ServiceDetailResponse,
  DeploymentsResponse,
  DrainStatus,
} from '../types'
import { fetch as compassFetch } from '@asty-web-app/compass'
import { apiPaths } from '@/lib/routes'
import { apiURL } from '@/lib/backend'

// authToken returns the Bearer token the client attaches to write
// requests. We pull it from VITE_ASTY_TOKEN at build time and from
// window.__ASTY_TOKEN__ at runtime (set by an inline script in
// index.html before the bundle loads). Reads happen per request so a
// future rotate-without-reload becomes a one-liner.
function authToken(): string {
  if (typeof window !== 'undefined' && (window as { __ASTY_TOKEN__?: string }).__ASTY_TOKEN__) {
    return (window as { __ASTY_TOKEN__?: string }).__ASTY_TOKEN__ as string
  }
  return (import.meta.env?.VITE_ASTY_TOKEN as string) ?? ''
}

// ApiError carries the HTTP status (0 = the fetch itself never
// completed, e.g. the cluster is unreachable) so the toast layer can
// render a localized message keyed off the status instead of the raw
// English statusText.
export class ApiError extends Error {
  readonly status: number
  constructor(status: number) {
    super(status === 0 ? 'Network error' : `HTTP ${status}`)
    this.name = 'ApiError'
    this.status = status
  }
}

async function fetchJSON<T>(url: string, options?: RequestInit): Promise<T> {
  const headers = new Headers(options?.headers)
  const token = authToken()
  if (token && !headers.has('Authorization')) {
    headers.set('Authorization', `Bearer ${token}`)
  }
  let response: Response
  try {
    response = await compassFetch(apiURL(url), { ...options, headers })
  } catch {
    throw new ApiError(0)
  }
  if (!response.ok) {
    throw new ApiError(response.status)
  }
  return response.json()
}

export const api = {
  // Services
  getService: (name: string) =>
    fetchJSON<ServiceDetailResponse>(apiPaths.service(name)),
  scaleService: (name: string, count: number) =>
    fetchJSON(apiPaths.serviceScale(name), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ count }),
    }),

  // Allocations (now scoped under their hosting node)
  getAllocation: (nodeId: string, allocId: string) =>
    fetchJSON<AllocationDetail>(apiPaths.allocation(nodeId, allocId)),
  getAllocationLogs: (nodeId: string, allocId: string) =>
    fetchJSON<LogsResponse>(apiPaths.allocationLogs(nodeId, allocId)),

  // Autoscaler — single per-service endpoint returns status + events.
  getServiceAutoscaler: (name: string) =>
    fetchJSON(apiPaths.serviceAutoscaler(name)),

  // Deploy
  deploy: (service: string, version: string) =>
    fetchJSON(apiPaths.serviceDeploy(service), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version }),
    }),
  getServiceDeployments: (service: string) =>
    fetchJSON<DeploymentsResponse>(apiPaths.serviceDeploy(service)),
  getServiceVersions: (service: string) =>
    fetchJSON<{ versions: string[]; error?: string }>(apiPaths.serviceVersions(service)),

  // Node maintenance
  drainNode: (id: string, enable: boolean) =>
    fetchJSON(apiPaths.nodeDrain(id), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enable }),
    }),
  getDrainStatus: (id: string) =>
    fetchJSON<DrainStatus>(apiPaths.nodeDrain(id)),
  pauseNode: (id: string) =>
    fetchJSON(apiPaths.nodePause(id), { method: 'POST' }),
  // killNode is the abrupt-decommission counterpart of drain. Body
  // carries `confirm_name` (must equal `id`); backend triggers the
  // agent's graceful self-shutdown via NATS and force-purges any KV
  // residue. Use Drain for normal operations — see Kill dialog copy.
  killNode: (id: string, confirmName: string) =>
    fetchJSON(apiPaths.nodeKill(id), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ confirm_name: confirmName }),
    }),
  // nodeExists probes GET /nodes/{id}: false on 404 (gone), true on 200.
  // Throws on any other status / network error so the caller can tell
  // "confirmed gone" apart from "couldn't reach a node to check".
  // Used to judge a kill by state — killing the leader tears down the
  // connection serving the request, so its HTTP outcome is unreliable.
  nodeExists: async (id: string): Promise<boolean> => {
    let res: Response
    try {
      res = await compassFetch(apiURL(apiPaths.node(id)))
    } catch {
      throw new ApiError(0)
    }
    if (res.status === 404) return false
    if (res.ok) return true
    throw new ApiError(res.status)
  },

  // Allocation actions
  restartAllocation: (nodeId: string, allocId: string) =>
    fetchJSON(apiPaths.allocationRestart(nodeId, allocId), { method: 'POST' }),
  stopAllocation: (nodeId: string, allocId: string) =>
    fetchJSON(apiPaths.allocationStop(nodeId, allocId), { method: 'POST' }),
}
