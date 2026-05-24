import type {
  AllocationDetail,
  LogsResponse,
  ServiceDetailResponse,
  DeploymentsResponse,
  DrainStatus,
} from '../types'
import { apiPaths } from '@/lib/routes'

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

async function fetchJSON<T>(url: string, options?: RequestInit): Promise<T> {
  const headers = new Headers(options?.headers)
  const token = authToken()
  if (token && !headers.has('Authorization')) {
    headers.set('Authorization', `Bearer ${token}`)
  }
  const response = await fetch(url, { ...options, headers })
  if (!response.ok) {
    throw new Error(`HTTP ${response.status}: ${response.statusText}`)
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

  // Allocation actions
  restartAllocation: (nodeId: string, allocId: string) =>
    fetchJSON(apiPaths.allocationRestart(nodeId, allocId), { method: 'POST' }),
  stopAllocation: (nodeId: string, allocId: string) =>
    fetchJSON(apiPaths.allocationStop(nodeId, allocId), { method: 'POST' }),
}
